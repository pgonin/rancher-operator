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
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	corev1alpha1 "github.com/pgonin/rancher-operator/api/v1alpha1"
)

const (
	requeueAfterSuccess = 5 * time.Minute
	requeueAfterError   = 30 * time.Second
	probeTimeout        = 15 * time.Second

	conditionTypeAvailable = "Available"

	reasonAvailable         = "Reachable"
	reasonMissingBYOSpec    = "MissingBYOSpec"
	reasonUnsupportedType   = "UnsupportedType"
	reasonMissingSecret     = "MissingSecret"
	reasonInvalidKubeconfig = "InvalidKubeconfig"
	reasonConnectionFailed  = "ConnectionFailed"

	defaultKubeconfigKey = "kubeconfig"
)

// TargetClusterReconciler reconciles a TargetCluster object
type TargetClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.rancher-operator.io,resources=targetclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.rancher-operator.io,resources=targetclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.rancher-operator.io,resources=targetclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *TargetClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var tc corev1alpha1.TargetCluster
	if err := r.Get(ctx, req.NamespacedName, &tc); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if tc.Spec.Type != corev1alpha1.TargetClusterTypeBYO {
		r.markUnavailable(&tc, reasonUnsupportedType,
			fmt.Sprintf("provisioning type %q is not implemented in v1alpha1", tc.Spec.Type))
		return ctrl.Result{}, r.writeStatus(ctx, &tc)
	}
	if tc.Spec.BYO == nil {
		r.markUnavailable(&tc, reasonMissingBYOSpec, `spec.byo must be set when spec.type is "byo"`)
		return ctrl.Result{}, r.writeStatus(ctx, &tc)
	}

	secretRef := tc.Spec.BYO.KubeconfigSecretRef
	key := secretRef.Key
	if key == "" {
		key = defaultKubeconfigKey
	}

	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: tc.Namespace, Name: secretRef.Name}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			r.markUnavailable(&tc, reasonMissingSecret,
				fmt.Sprintf("Secret %q not found in namespace %q", secretRef.Name, tc.Namespace))
			return ctrl.Result{RequeueAfter: requeueAfterError}, r.writeStatus(ctx, &tc)
		}
		return ctrl.Result{}, fmt.Errorf("fetching kubeconfig Secret: %w", err)
	}

	kubeconfigBytes, ok := secret.Data[key]
	if !ok || len(kubeconfigBytes) == 0 {
		r.markUnavailable(&tc, reasonInvalidKubeconfig,
			fmt.Sprintf("Secret %q has no data at key %q", secretRef.Name, key))
		return ctrl.Result{RequeueAfter: requeueAfterError}, r.writeStatus(ctx, &tc)
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		r.markUnavailable(&tc, reasonInvalidKubeconfig, fmt.Sprintf("kubeconfig is invalid: %v", err))
		return ctrl.Result{}, r.writeStatus(ctx, &tc)
	}
	restConfig.Timeout = probeTimeout

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		r.markUnavailable(&tc, reasonInvalidKubeconfig, fmt.Sprintf("cannot build client: %v", err))
		return ctrl.Result{}, r.writeStatus(ctx, &tc)
	}

	version, err := clientset.Discovery().ServerVersion()
	if err != nil {
		r.markUnavailable(&tc, reasonConnectionFailed, fmt.Sprintf("ServerVersion: %v", err))
		return ctrl.Result{RequeueAfter: requeueAfterError}, r.writeStatus(ctx, &tc)
	}

	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		r.markUnavailable(&tc, reasonConnectionFailed, fmt.Sprintf("listing Nodes: %v", err))
		return ctrl.Result{RequeueAfter: requeueAfterError}, r.writeStatus(ctx, &tc)
	}

	tc.Status.Ready = true
	tc.Status.KubernetesVersion = version.GitVersion
	tc.Status.NodeCount = int32(len(nodes.Items))
	tc.Status.ObservedGeneration = tc.Generation
	meta.SetStatusCondition(&tc.Status.Conditions, metav1.Condition{
		Type:               conditionTypeAvailable,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: tc.Generation,
		Reason:             reasonAvailable,
		Message:            fmt.Sprintf("target cluster reachable (%d nodes, %s)", len(nodes.Items), version.GitVersion),
	})
	if err := r.writeStatus(ctx, &tc); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("target cluster healthy", "version", version.GitVersion, "nodes", len(nodes.Items))
	return ctrl.Result{RequeueAfter: requeueAfterSuccess}, nil
}

func (r *TargetClusterReconciler) markUnavailable(tc *corev1alpha1.TargetCluster, reason, message string) {
	tc.Status.Ready = false
	tc.Status.ObservedGeneration = tc.Generation
	meta.SetStatusCondition(&tc.Status.Conditions, metav1.Condition{
		Type:               conditionTypeAvailable,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: tc.Generation,
		Reason:             reason,
		Message:            message,
	})
}

func (r *TargetClusterReconciler) writeStatus(ctx context.Context, tc *corev1alpha1.TargetCluster) error {
	return r.Status().Update(ctx, tc)
}

// SetupWithManager sets up the controller with the Manager.
func (r *TargetClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.TargetCluster{}).
		Named("targetcluster").
		Complete(r)
}
