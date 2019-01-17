package restore

import (
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/heptio/ark/pkg/apis/ark/v1"
	"github.com/heptio/ark/pkg/restore"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// FunctionPluginRestore is a plugin for ark to restore several fields before creating restored object
type FunctionPluginRestore struct {
	Log logrus.FieldLogger
}

// AppliesTo return list of resource kinds which should be handled by this plugin
func (p *FunctionPluginRestore) AppliesTo() (restore.ResourceSelector, error) {
	return restore.ResourceSelector{IncludedResources: []string{"all", "serviceinstance", "servicebinding", "servicebindingusage", "function", "subscription", "api", "eventactivation"},
		ExcludedNamespaces: []string{"default", "heptio-ark", "istio-system", "kube-public", "kube-system", "kyma-installer", "kyma-integration", "kyma-system"},
		LabelSelector:      "function",
	}, nil
}

// Execute sets a custom annotation on the item being restored.
// nolint
func (p *FunctionPluginRestore) Execute(item runtime.Unstructured, restore *v1.Restore) (runtime.Unstructured, error, error) {
	metadata, err := meta.Accessor(item)
	if err != nil {
		return nil, nil, err
	}

	p.Log.Infof("Removing serviceClassRef/servicePlanRef fields from instance %s in namespace %s", metadata.GetName(), metadata.GetNamespace())
	unstructured.RemoveNestedField(item.UnstructuredContent(), "spec", "serviceClassRef")
	unstructured.RemoveNestedField(item.UnstructuredContent(), "spec", "servicePlanRef")

	return item, nil, nil
}
