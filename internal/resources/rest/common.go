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
	corev1 "k8s.io/api/core/v1"
)

// convertResources converts K8s ResourceRequirements to unstructured format
func convertResources(res *corev1.ResourceRequirements) map[string]interface{} {
	result := make(map[string]interface{})

	if len(res.Requests) > 0 {
		requests := make(map[string]interface{})
		for k, v := range res.Requests {
			requests[string(k)] = v.String()
		}
		result["requests"] = requests
	}

	if len(res.Limits) > 0 {
		limits := make(map[string]interface{})
		for k, v := range res.Limits {
			limits[string(k)] = v.String()
		}
		result["limits"] = limits
	}

	return result
}
