/*
Copyright 2018 The Kubernetes Authors.

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

package e2e

import (
	"fmt"

	"github.com/pkg/errors"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/test/e2e/framework"
)

func init() {
	framework.Factories[framework.What{"Secret"}] = &SecretFactory{}
}

type SecretFactory struct{}

func (f *SecretFactory) New() runtime.Object {
	return &v1.Secret{}
}

func (*SecretFactory) Create(f *framework.Framework, i interface{}) (func() error, error) {
	item, ok := i.(*v1.Secret)
	if !ok {
		return nil, framework.ItemNotSupported
	}

	client := f.ClientSet.CoreV1().Secrets(f.Namespace.GetName())
	if _, err := client.Create(item); err != nil {
		return nil, errors.Wrap(err, "create Secret")
	}
	return func() error {
		return client.Delete(item.GetName(), &metav1.DeleteOptions{})
	}, nil
}

func (*SecretFactory) UniqueName(i interface{}) string {
	item, ok := i.(*v1.Secret)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s/%s", item.GetNamespace(), item.GetName())
}
