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
	"fmt"

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
	prereqMessageNotImplemented    = "installer not yet implemented in this build"
	prereqMessageComponentDisabled = "component disabled by spec"
	prereqMessageComponentNotSet   = "component not requested in spec"
	prereqMessageLoadBalancerCloud = "no installation required when type is cloud"

	certManagerRepoURL        = "https://charts.jetstack.io"
	certManagerChartName      = "cert-manager"
	certManagerReleaseName    = "cert-manager"
	certManagerNamespace      = "cert-manager"
	defaultCertManagerVersion = "v1.18.2"
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

	cp.Status.Components.CertManager = r.reconcileCertManager(ctx, helmGetter, cp.Spec.CertManager)
	cp.Status.Components.Ingress = ingressComponentStatus(cp.Spec.Ingress)
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

// reconcileCertManager installs or upgrades cert-manager on the target cluster
// via Helm, then returns a ComponentStatus reflecting the outcome. Failures
// surface as Ready=false with a Message; the next reconcile retries.
func (r *ClusterPrerequisitesReconciler) reconcileCertManager(
	ctx context.Context,
	getter *helm.KubeconfigRESTClientGetter,
	spec *corev1alpha1.CertManagerComponent,
) corev1alpha1.ComponentStatus {
	if spec == nil {
		return corev1alpha1.ComponentStatus{Skipped: true, Message: prereqMessageComponentNotSet}
	}
	if !spec.Enabled {
		return corev1alpha1.ComponentStatus{Skipped: true, Message: prereqMessageComponentDisabled}
	}

	version := spec.Version
	if version == "" {
		version = defaultCertManagerVersion
	}

	chart, err := helm.LoadChartFromRepo(ctx, certManagerRepoURL, certManagerChartName, version)
	if err != nil {
		return corev1alpha1.ComponentStatus{Message: fmt.Sprintf("loading chart: %v", err)}
	}

	rel, err := helm.InstallOrUpgrade(ctx, getter, helm.InstallOrUpgradeOptions{
		ReleaseName:     certManagerReleaseName,
		Namespace:       certManagerNamespace,
		CreateNamespace: true,
		Chart:           chart,
		Values: map[string]any{
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
		Message:          "cert-manager installed",
	}
}

// resolveTargetKubeconfig pulls the kubeconfig bytes from the Secret referenced
// by the TargetCluster's BYO spec. Returns an error if the TargetCluster is not
// using the BYO mode or the Secret cannot be read.
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

func ingressComponentStatus(spec *corev1alpha1.IngressComponent) corev1alpha1.ComponentStatus {
	if spec == nil {
		return corev1alpha1.ComponentStatus{Skipped: true, Message: prereqMessageComponentNotSet}
	}
	if !spec.Enabled {
		return corev1alpha1.ComponentStatus{Skipped: true, Message: prereqMessageComponentDisabled}
	}
	return corev1alpha1.ComponentStatus{Message: prereqMessageNotImplemented}
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
