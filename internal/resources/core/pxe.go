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

package core

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources/tls"
)

const (
	PXEName = "carbide-pxe"
)

// BuildPXEDeployment creates the PXE boot server Deployment (HTTP iPXE only, no TFTP).
func BuildPXEDeployment(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *appsv1.Deployment {
	labels := resources.DefaultLabels("pxe", deployment)
	labels["app"] = PXEName

	registry := resources.GetImageRegistry(deployment)
	imageName := fmt.Sprintf("%s/carbide-core:%s", registry, deployment.Spec.Version)
	if deployment.Spec.Images != nil && deployment.Spec.Images.PXE != "" {
		imageName = deployment.Spec.Images.PXE
	}

	httpPort := deployment.Spec.Core.PXE.HTTPPort
	if httpPort == 0 {
		httpPort = 8080
	}

	replicas := int32(1)

	res := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("200m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		},
	}
	if deployment.Spec.Core.PXE.Resources != nil {
		res = *deployment.Spec.Core.PXE.Resources
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "spiffe",
			MountPath: "/var/run/secrets/spiffe.io",
			ReadOnly:  true,
		},
	}

	volumes := []corev1.Volume{}

	// TLS cert volume — in cert-manager mode, mount the certificate secret;
	// in SPIFFE mode, the TLS injection adds CSI volumes
	if tls.IsCertManagerMode(deployment) {
		volumes = append(volumes, corev1.Volume{
			Name: "spiffe",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: PXEName + "-certificate",
				},
			},
		})
	}

	podSpec := corev1.PodSpec{
		ServiceAccountName: PXEName,
		Containers: []corev1.Container{
			{
				Name:            "carbide-pxe",
				Image:           imageName,
				ImagePullPolicy: resources.GetImagePullPolicy(deployment),
				Command:         []string{"/opt/carbide/carbide"},
				Args:            []string{"-s", "/forge-boot-artifacts"},
				Ports: []corev1.ContainerPort{
					{
						Name:          "http",
						ContainerPort: httpPort,
						Protocol:      corev1.ProtocolTCP,
					},
				},
				VolumeMounts: volumeMounts,
				Resources:    res,
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/healthz",
							Port: intstr.FromInt32(httpPort),
						},
					},
					InitialDelaySeconds: 15,
					PeriodSeconds:       20,
				},
			},
		},
		Volumes: volumes,
	}

	tls.InjectTLS(&podSpec, deployment)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PXEName,
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
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": PXEName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: podSpec,
			},
		},
	}
}

// BuildPXEPVC creates the PersistentVolumeClaim for PXE boot files.
func BuildPXEPVC(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *corev1.PersistentVolumeClaim {
	pxeConfig := deployment.Spec.Core.PXE

	storageSize := resource.MustParse("50Gi")
	if pxeConfig.Storage != nil {
		storageSize = pxeConfig.Storage.Size
	}

	storageClass := resources.GetStorageClass(deployment, pxeConfig.Storage)

	accessMode := corev1.ReadWriteOnce
	if pxeConfig.Storage != nil && pxeConfig.Storage.AccessMode != "" {
		accessMode = pxeConfig.Storage.AccessMode
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-data", PXEName),
			Namespace: namespace,
			Labels:    resources.DefaultLabels("pxe-storage", deployment),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
					Group:   carbitev1alpha1.GroupVersion.Group,
					Version: carbitev1alpha1.GroupVersion.Version,
					Kind:    "CarbideDeployment",
				}),
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{accessMode},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageSize,
				},
			},
		},
	}

	if storageClass != "" {
		pvc.Spec.StorageClassName = &storageClass
	}

	return pvc
}
