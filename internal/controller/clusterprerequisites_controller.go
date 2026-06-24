/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	corev1alpha1 "github.com/pgonin/rancher-operator/api/v1alpha1"
	"github.com/pgonin/rancher-operator/internal/helm"
)

const (
	prereqReasonAvailable          = "AllComponentsReady"
	prereqReasonComponentsPending  = "ComponentsNotReady"
	prereqReasonMissingTarget      = "MissingTargetCluster"
	prereqReasonTargetNotReady     = "TargetClusterNotReady"
	prereqReasonKubeconfigMissing  = "MissingKubeconfig"
	prereqMessageComponentDisabled = "component disabled by spec"
	prereqMessageComponentNotSet   = "component not requested in spec"
	prereqMessageLoadBalancerCloud = "no installation required when type is cloud"
	prereqMessageNotImplemented    = "installer not yet implemented in this build"
	prereqMessageMissingCreds      = "spec.applicationCollectionCredentialsSecretRef is required to install this component from the SUSE Application Collection"

	// SUSE Application Collection registry where Helm charts and container
	// images are served. The image-pull Secret name follows SUSE's docs
	// convention so chart values can reference it without configuration.
	applicationCollectionRegistry        = "dp.apps.rancher.io"
	applicationCollectionImagePullSecret = "application-collection"

	certManagerChartRef       = "oci://dp.apps.rancher.io/charts/cert-manager"
	certManagerReleaseName    = "cert-manager"
	certManagerNamespace      = "cert-manager"
	defaultCertManagerVersion = "v1.18.2"

	traefikChartRef       = "oci://dp.apps.rancher.io/charts/traefik"
	traefikReleaseName    = "traefik"
	traefikNamespace      = "traefik"
	defaultTraefikVersion = "37.0.0"
)

// ClusterPrerequisitesReconciler reconciles a ClusterPrerequisites object
type ClusterPrerequisitesReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.rancher-operator.io,resources=clusterprerequisites,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.rancher-operator.io,resources=clusterprerequisites/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.rancher-operator.io,resources=clusterprerequisites/finalizers,verbs=update
// +kubebuilder:rbac:groups=core.rancher-operator.io,resources=targetclusters,verbs=get;list;watch

func (r *ClusterPrerequisitesReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var cp corev1alpha1.ClusterPrerequisites
	if err := r.Get(ctx, req.NamespacedName, &cp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Resolve the referenced TargetCluster in the same namespace.
	var tc corev1alpha1.TargetCluster
	tcKey := types.NamespacedName{Namespace: cp.Namespace, Name: cp.Spec.TargetClusterRef.Name}
	if err := r.Get(ctx, tcKey, &tc); err != nil {
		if apierrors.IsNotFound(err) {
			r.markUnavailable(&cp, prereqReasonMissingTarget,
				fmt.Sprintf("TargetCluster %q not found in namespace %q", cp.Spec.TargetClusterRef.Name, cp.Namespace))
			return ctrl.Result{RequeueAfter: requeueAfterError}, r.writePrereqStatus(ctx, &cp)
		}
		return ctrl.Result{}, fmt.Errorf("fetching TargetCluster: %w", err)
	}
	if !tc.Status.Ready {
		r.markUnavailable(&cp, prereqReasonTargetNotReady,
			fmt.Sprintf("TargetCluster %q is not Ready", tc.Name))
		return ctrl.Result{RequeueAfter: requeueAfterError}, r.writePrereqStatus(ctx, &cp)
	}

	// Fetch the target cluster's kubeconfig via its BYO Secret. Only the BYO
	// provisioning mode is implemented in v1alpha1; other modes are gated
	// earlier in the TargetCluster reconciler.
	kubeconfig, err := r.resolveTargetKubeconfig(ctx, &tc)
	if err != nil {
		r.markUnavailable(&cp, prereqReasonKubeconfigMissing, err.Error())
		return ctrl.Result{RequeueAfter: requeueAfterError}, r.writePrereqStatus(ctx, &cp)
	}
	helmGetter := helm.NewKubeconfigRESTClientGetter(kubeconfig)

	// SUSE Application Collection credentials are optional in the spec but
	// required by any component that pulls from dp.apps.rancher.io. Components
	// that don't need them (skipped, bundled, cloud LB, etc.) ignore the empty
	// value; installers surface a clear error when creds are missing.
	creds, _ := r.loadApplicationCollectionCredentials(ctx, &cp)

	cp.Status.Components.CertManager = r.reconcileCertManager(ctx, helmGetter, cp.Spec.CertManager, creds)
	cp.Status.Components.Ingress = r.reconcileIngress(ctx, helmGetter, cp.Spec.Ingress, tc.Status.KubernetesVersion, creds)
	cp.Status.Components.LoadBalancer = loadBalancerComponentStatus(cp.Spec.LoadBalancer)

	allReady := isComponentDone(cp.Status.Components.CertManager) &&
		isComponentDone(cp.Status.Components.Ingress) &&
		isComponentDone(cp.Status.Components.LoadBalancer)
	cp.Status.Ready = allReady
	cp.Status.ObservedGeneration = cp.Generation

	cond := metav1.Condition{
		Type:               conditionTypeAvailable,
		ObservedGeneration: cp.Generation,
	}
	if allReady {
		cond.Status = metav1.ConditionTrue
		cond.Reason = prereqReasonAvailable
		cond.Message = "all requested components are installed and healthy"
	} else {
		cond.Status = metav1.ConditionFalse
		cond.Reason = prereqReasonComponentsPending
		cond.Message = "one or more components are not yet ready"
	}
	meta.SetStatusCondition(&cp.Status.Conditions, cond)

	if err := r.writePrereqStatus(ctx, &cp); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("clusterprerequisites reconciled", "ready", allReady)
	return ctrl.Result{RequeueAfter: requeueAfterSuccess}, nil
}

// reconcileCertManager installs or upgrades cert-manager from the SUSE
// Application Collection. The function ensures an image-pull Secret exists in
// the cert-manager namespace before invoking Helm so that chart pods can pull
// their container images from dp.apps.rancher.io.
func (r *ClusterPrerequisitesReconciler) reconcileCertManager(
	ctx context.Context,
	getter *helm.KubeconfigRESTClientGetter,
	spec *corev1alpha1.CertManagerComponent,
	creds helm.OCICredentials,
) corev1alpha1.ComponentStatus {
	if spec == nil {
		return corev1alpha1.ComponentStatus{Skipped: true, Message: prereqMessageComponentNotSet}
	}
	if !spec.Enabled {
		return corev1alpha1.ComponentStatus{Skipped: true, Message: prereqMessageComponentDisabled}
	}
	if creds.Username == "" || creds.Password == "" {
		return corev1alpha1.ComponentStatus{Message: prereqMessageMissingCreds}
	}

	version := spec.Version
	if version == "" {
		version = defaultCertManagerVersion
	}

	if err := helm.EnsureImagePullSecret(ctx, getter, certManagerNamespace, applicationCollectionImagePullSecret,
		applicationCollectionRegistry, creds.Username, creds.Password); err != nil {
		return corev1alpha1.ComponentStatus{Message: fmt.Sprintf("image-pull Secret: %v", err)}
	}

	chart, err := helm.LoadChartFromOCI(ctx, certManagerChartRef, version, creds)
	if err != nil {
		return corev1alpha1.ComponentStatus{Message: fmt.Sprintf("loading chart: %v", err)}
	}

	rel, err := helm.InstallOrUpgrade(ctx, getter, helm.InstallOrUpgradeOptions{
		ReleaseName:     certManagerReleaseName,
		Namespace:       certManagerNamespace,
		CreateNamespace: true,
		Chart:           chart,
		Values: map[string]any{
			"global": map[string]any{
				"imagePullSecrets": []any{applicationCollectionImagePullSecret},
			},
			"crds": map[string]any{"enabled": true},
		},
	})
	if err != nil {
		return corev1alpha1.ComponentStatus{Message: fmt.Sprintf("helm: %v", err)}
	}

	installed := version
	if rel != nil && rel.Chart != nil && rel.Chart.Metadata != nil && rel.Chart.Metadata.Version != "" {
		installed = rel.Chart.Metadata.Version
	}
	return corev1alpha1.ComponentStatus{
		Ready:            true,
		InstalledVersion: installed,
		Message:          "cert-manager installed from the SUSE Application Collection",
	}
}

// reconcileIngress installs Traefik from the SUSE Application Collection, or
// defers to a cluster-bundled copy. Detection in "auto" mode is keyed off the
// target cluster's Kubernetes version string carrying a "+k3s" or "+rke2"
// suffix, which K3s and RKE2 both stamp into the server version.
func (r *ClusterPrerequisitesReconciler) reconcileIngress(
	ctx context.Context,
	getter *helm.KubeconfigRESTClientGetter,
	spec *corev1alpha1.IngressComponent,
	k8sVersion string,
	creds helm.OCICredentials,
) corev1alpha1.ComponentStatus {
	if spec == nil {
		return corev1alpha1.ComponentStatus{Skipped: true, Message: prereqMessageComponentNotSet}
	}
	if !spec.Enabled {
		return corev1alpha1.ComponentStatus{Skipped: true, Message: prereqMessageComponentDisabled}
	}

	mode := spec.UseBundled
	if mode == "" {
		mode = corev1alpha1.IngressBundledModeAuto
	}
	switch mode {
	case corev1alpha1.IngressBundledModeUse:
		return corev1alpha1.ComponentStatus{Ready: true, Skipped: true, Message: "using the cluster-bundled ingress controller (spec.ingress.useBundled=use)"}
	case corev1alpha1.IngressBundledModeAuto:
		if hasBundledTraefik(k8sVersion) {
			return corev1alpha1.ComponentStatus{
				Ready:            true,
				Skipped:          true,
				InstalledVersion: k8sVersion,
				Message:          fmt.Sprintf("using bundled Traefik (Kubernetes version %q matches K3s/RKE2)", k8sVersion),
			}
		}
	case corev1alpha1.IngressBundledModeInstall:
		// fall through to install
	}

	if creds.Username == "" || creds.Password == "" {
		return corev1alpha1.ComponentStatus{Message: prereqMessageMissingCreds}
	}

	version := spec.Version
	if version == "" {
		version = defaultTraefikVersion
	}

	if err := helm.EnsureImagePullSecret(ctx, getter, traefikNamespace, applicationCollectionImagePullSecret,
		applicationCollectionRegistry, creds.Username, creds.Password); err != nil {
		return corev1alpha1.ComponentStatus{Message: fmt.Sprintf("image-pull Secret: %v", err)}
	}

	chart, err := helm.LoadChartFromOCI(ctx, traefikChartRef, version, creds)
	if err != nil {
		return corev1alpha1.ComponentStatus{Message: fmt.Sprintf("loading chart: %v", err)}
	}

	rel, err := helm.InstallOrUpgrade(ctx, getter, helm.InstallOrUpgradeOptions{
		ReleaseName:     traefikReleaseName,
		Namespace:       traefikNamespace,
		CreateNamespace: true,
		Chart:           chart,
		Values: map[string]any{
			"global": map[string]any{
				"imagePullSecrets": []any{applicationCollectionImagePullSecret},
			},
		},
	})
	if err != nil {
		return corev1alpha1.ComponentStatus{Message: fmt.Sprintf("helm: %v", err)}
	}

	installed := version
	if rel != nil && rel.Chart != nil && rel.Chart.Metadata != nil && rel.Chart.Metadata.Version != "" {
		installed = rel.Chart.Metadata.Version
	}
	return corev1alpha1.ComponentStatus{
		Ready:            true,
		InstalledVersion: installed,
		Message:          "Traefik installed from the SUSE Application Collection",
	}
}

// hasBundledTraefik returns true when the Kubernetes server version reports a
// K3s or RKE2 build suffix. Both distros ship Traefik out of the box and
// re-installing it would collide with the bundled HelmChart resources.
func hasBundledTraefik(k8sVersion string) bool {
	return strings.Contains(k8sVersion, "+k3s") || strings.Contains(k8sVersion, "+rke2")
}

// loadApplicationCollectionCredentials reads username/password from the Secret
// referenced by spec.applicationCollectionCredentialsSecretRef. Returns the
// zero value (no error) when the ref is absent, so callers can branch on creds
// presence at the per-component level.
func (r *ClusterPrerequisitesReconciler) loadApplicationCollectionCredentials(
	ctx context.Context,
	cp *corev1alpha1.ClusterPrerequisites,
) (helm.OCICredentials, error) {
	if cp.Spec.ApplicationCollectionCredentialsSecretRef == nil {
		return helm.OCICredentials{}, nil
	}
	name := cp.Spec.ApplicationCollectionCredentialsSecretRef.Name
	var s corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: cp.Namespace, Name: name}, &s); err != nil {
		return helm.OCICredentials{}, fmt.Errorf("reading SUSE Application Collection credentials Secret %q: %w", name, err)
	}
	username := string(s.Data["username"])
	password := string(s.Data["password"])
	if username == "" || password == "" {
		return helm.OCICredentials{}, errors.New(`SUSE Application Collection credentials Secret must contain "username" and "password" keys`)
	}
	return helm.OCICredentials{Username: username, Password: password}, nil
}

// resolveTargetKubeconfig pulls the kubeconfig bytes from the Secret referenced
// by the TargetCluster's BYO spec.
func (r *ClusterPrerequisitesReconciler) resolveTargetKubeconfig(ctx context.Context, tc *corev1alpha1.TargetCluster) ([]byte, error) {
	if tc.Spec.Type != corev1alpha1.TargetClusterTypeBYO || tc.Spec.BYO == nil {
		return nil, fmt.Errorf("TargetCluster %q is not in byo mode", tc.Name)
	}
	ref := tc.Spec.BYO.KubeconfigSecretRef
	key := ref.Key
	if key == "" {
		key = "kubeconfig"
	}

	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: tc.Namespace, Name: ref.Name}, &secret); err != nil {
		return nil, fmt.Errorf("reading kubeconfig Secret %q: %w", ref.Name, err)
	}
	data, ok := secret.Data[key]
	if !ok || len(data) == 0 {
		return nil, fmt.Errorf("Secret %q has no data at key %q", ref.Name, key)
	}
	return data, nil
}

func loadBalancerComponentStatus(spec *corev1alpha1.LoadBalancerComponent) corev1alpha1.ComponentStatus {
	if spec == nil {
		return corev1alpha1.ComponentStatus{Skipped: true, Message: prereqMessageComponentNotSet}
	}
	switch spec.Type {
	case corev1alpha1.LoadBalancerTypeNone:
		return corev1alpha1.ComponentStatus{Skipped: true, Message: "type is none; user-provided load balancer"}
	case corev1alpha1.LoadBalancerTypeCloud:
		// Nothing to install — cloud-controller-manager handles LoadBalancer Services.
		return corev1alpha1.ComponentStatus{Ready: true, Message: prereqMessageLoadBalancerCloud}
	default:
		return corev1alpha1.ComponentStatus{Message: prereqMessageNotImplemented}
	}
}

// isComponentDone is true when a component is either successfully installed or
// intentionally skipped — i.e. it does not block overall readiness.
func isComponentDone(s corev1alpha1.ComponentStatus) bool {
	return s.Ready || s.Skipped
}

func (r *ClusterPrerequisitesReconciler) markUnavailable(cp *corev1alpha1.ClusterPrerequisites, reason, message string) {
	cp.Status.Ready = false
	cp.Status.ObservedGeneration = cp.Generation
	meta.SetStatusCondition(&cp.Status.Conditions, metav1.Condition{
		Type:               conditionTypeAvailable,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: cp.Generation,
		Reason:             reason,
		Message:            message,
	})
}

func (r *ClusterPrerequisitesReconciler) writePrereqStatus(ctx context.Context, cp *corev1alpha1.ClusterPrerequisites) error {
	return r.Status().Update(ctx, cp)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterPrerequisitesReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.ClusterPrerequisites{}).
		Named("clusterprerequisites").
		Complete(r)
}
