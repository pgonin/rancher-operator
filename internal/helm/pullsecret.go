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

package helm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// EnsureImagePullSecret materialises a dockerconfigjson Secret in the given
// namespace on the target cluster (creating the namespace too if needed).
// The Secret name is what the caller threads into a chart's
// global.imagePullSecrets list. If the Secret already exists with identical
// contents, this is a no-op; otherwise it is updated.
func EnsureImagePullSecret(
	ctx context.Context,
	getter *KubeconfigRESTClientGetter,
	namespace, secretName, registryHost, username, password string,
) error {
	cfg, err := getter.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("resolving target REST config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("building target clientset: %w", err)
	}

	if err := ensureNamespace(ctx, clientset, namespace); err != nil {
		return err
	}

	desired, err := dockerconfigjson(registryHost, username, password)
	if err != nil {
		return err
	}

	existing, err := clientset.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		_, err := clientset.CoreV1().Secrets(namespace).Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Type:       corev1.SecretTypeDockerConfigJson,
			Data:       map[string][]byte{corev1.DockerConfigJsonKey: desired},
		}, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("creating image-pull Secret %s/%s: %w", namespace, secretName, err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("looking up image-pull Secret %s/%s: %w", namespace, secretName, err)
	}

	if existing.Type == corev1.SecretTypeDockerConfigJson && bytesEqual(existing.Data[corev1.DockerConfigJsonKey], desired) {
		return nil
	}

	existing.Type = corev1.SecretTypeDockerConfigJson
	if existing.Data == nil {
		existing.Data = map[string][]byte{}
	}
	existing.Data[corev1.DockerConfigJsonKey] = desired
	if _, err := clientset.CoreV1().Secrets(namespace).Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating image-pull Secret %s/%s: %w", namespace, secretName, err)
	}
	return nil
}

func ensureNamespace(ctx context.Context, clientset *kubernetes.Clientset, name string) error {
	_, err := clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("looking up namespace %q: %w", name, err)
	}
	_, err = clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating namespace %q: %w", name, err)
	}
	return nil
}

func dockerconfigjson(registryHost, username, password string) ([]byte, error) {
	type authEntry struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Auth     string `json:"auth"`
	}
	cfg := struct {
		Auths map[string]authEntry `json:"auths"`
	}{
		Auths: map[string]authEntry{
			registryHost: {
				Username: username,
				Password: password,
				Auth:     base64.StdEncoding.EncodeToString([]byte(username + ":" + password)),
			},
		},
	}
	out, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshalling dockerconfigjson: %w", err)
	}
	return out, nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
