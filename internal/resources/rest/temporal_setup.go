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

package rest

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources"
)

const (
	TemporalSetupJobName = "temporal-namespace-init"
)

// BuildTemporalSetupJob creates a Job to initialize Temporal namespaces
func BuildTemporalSetupJob(deployment *carbitev1alpha1.CarbideDeployment, namespace, temporalEndpoint string) *batchv1.Job {
	labels := resources.DefaultLabels("temporal-setup", deployment)

	// Use official Temporal admin tools image
	imageName := "temporalio/admin-tools:latest"

	backoffLimit := int32(5)
	ttlSecondsAfterFinished := int32(86400) // 24 hours

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TemporalSetupJobName,
			Namespace: namespace,
			Labels:    labels,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
					Group:   carbitev1alpha1.GroupVersion.Group,
					Version: carbitev1alpha1.GroupVersion.Version,
					Kind:    "CarbideDeployment",
				}),
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttlSecondsAfterFinished,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:            "namespace-init",
							Image:           imageName,
							ImagePullPolicy: resources.GetImagePullPolicy(deployment),
							Command:         []string{"/bin/bash"},
							Args: []string{
								"-c",
								fmt.Sprintf(`
# Wait for Temporal frontend to be ready
echo "Waiting for Temporal to be ready..."
until tctl --address %s cluster health; do
  echo "Still waiting for Temporal..."
  sleep 5
done

echo "Temporal is ready, registering namespaces..."

# Register standard Temporal namespaces
tctl --address %s namespace register carbide --retention 7 || true
tctl --address %s namespace register default --retention 7 || true
tctl --address %s namespace register site --retention 7 || true
tctl --address %s namespace register cloud --retention 7 || true

echo "Temporal namespaces initialized successfully"
`, temporalEndpoint, temporalEndpoint, temporalEndpoint, temporalEndpoint, temporalEndpoint),
							},
						},
					},
				},
			},
		},
	}
}
