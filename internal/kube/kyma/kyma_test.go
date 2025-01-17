package kyma

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamic_fake "k8s.io/client-go/dynamic/fake"
)

func TestGetDefaultKyma(t *testing.T) {
	t.Run("get Kyma from the cluster", func(t *testing.T) {
		scheme := runtime.NewScheme()
		scheme.AddKnownTypes(GVRKyma.GroupVersion())
		client := NewClient(dynamic_fake.NewSimpleDynamicClient(scheme, fixDefaultKyma()))

		expectedKyma := &Kyma{
			TypeMeta: v1.TypeMeta{
				APIVersion: "operator.kyma-project.io/v1beta2",
				Kind:       "Kyma",
			},
			ObjectMeta: v1.ObjectMeta{
				Name:      "default",
				Namespace: "kyma-system",
			},
			Spec: KymaSpec{
				Channel: "fast",
				Modules: []Module{
					{
						Name: "test-module",
					},
				},
			},
		}

		kyma, err := client.GetDefaultKyma(context.Background())
		require.NoError(t, err)
		require.Equal(t, expectedKyma, kyma)
	})

	t.Run("kyma not found", func(t *testing.T) {
		scheme := runtime.NewScheme()
		scheme.AddKnownTypes(GVRKyma.GroupVersion())
		client := NewClient(dynamic_fake.NewSimpleDynamicClient(scheme))

		kyma, err := client.GetDefaultKyma(context.Background())
		require.ErrorContains(t, err, "not found")
		require.Nil(t, kyma)
	})
}

func TestUpdateDefaultKyma(t *testing.T) {
	t.Run("update kyma", func(t *testing.T) {
		scheme := runtime.NewScheme()
		scheme.AddKnownTypes(GVRKyma.GroupVersion())
		dynamic := dynamic_fake.NewSimpleDynamicClient(scheme, fixDefaultKyma())
		client := NewClient(dynamic)

		expectedKyma := &Kyma{
			TypeMeta: v1.TypeMeta{
				APIVersion: "operator.kyma-project.io/v1beta2",
				Kind:       "Kyma",
			},
			ObjectMeta: v1.ObjectMeta{
				Name:      "default",
				Namespace: "kyma-system",
			},
			Spec: KymaSpec{
				Channel: "fast",
				Modules: []Module{
					{
						Name:    "test-module",
						Channel: "regular",
					},
					{
						Name: "another-test-module",
					},
				},
			},
		}

		err := client.UpdateDefaultKyma(context.Background(), expectedKyma)
		require.NoError(t, err)

		u, err := dynamic.Resource(GVRKyma).Namespace("kyma-system").Get(context.Background(), "default", v1.GetOptions{})
		require.NoError(t, err)

		kyma := &Kyma{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, kyma)
		require.NoError(t, err)
		require.Equal(t, expectedKyma, kyma)
	})

	t.Run("kyma not found", func(t *testing.T) {
		scheme := runtime.NewScheme()
		scheme.AddKnownTypes(GVRKyma.GroupVersion())
		client := NewClient(dynamic_fake.NewSimpleDynamicClient(scheme))

		expectedKyma := &Kyma{
			TypeMeta: v1.TypeMeta{
				APIVersion: "operator.kyma-project.io/v1beta2",
				Kind:       "Kyma",
			},
			ObjectMeta: v1.ObjectMeta{
				Name:      "default",
				Namespace: "kyma-system",
			},
			Spec: KymaSpec{
				Channel: "fast",
				Modules: []Module{
					{
						Name:    "test-module",
						Channel: "regular",
					},
					{
						Name: "another-test-module",
					},
				},
			},
		}

		err := client.UpdateDefaultKyma(context.Background(), expectedKyma)
		require.ErrorContains(t, err, "not found")
	})
}

func Test_disableModule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		kymaCR     *Kyma
		moduleName string
		want       *Kyma
	}{
		{
			name: "unchanged modules list",
			kymaCR: &Kyma{
				Spec: KymaSpec{
					Modules: []Module{
						{
							Name: "istio",
						},
					},
				},
			},
			moduleName: "module",
			want: &Kyma{
				Spec: KymaSpec{
					Modules: []Module{
						{
							Name: "istio",
						},
					},
				},
			},
		},
		{
			name: "changed modules list",
			kymaCR: &Kyma{
				Spec: KymaSpec{
					Modules: []Module{
						{
							Name: "istio",
						},
						{
							Name: "module",
						},
					},
				},
			},
			moduleName: "module",
			want: &Kyma{
				Spec: KymaSpec{
					Modules: []Module{
						{
							Name: "istio",
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		kymaCR := tt.kymaCR
		moduleName := tt.moduleName
		want := tt.want
		t.Run(tt.name, func(t *testing.T) {
			got := disableModule(kymaCR, moduleName)
			gotBytes, err := json.Marshal(got)
			require.NoError(t, err)
			wantBytes, err := json.Marshal(want)
			require.NoError(t, err)
			var gotInterface map[string]interface{}
			var wantInterface map[string]interface{}
			err = json.Unmarshal(gotBytes, &gotInterface)
			require.NoError(t, err)
			err = json.Unmarshal(wantBytes, &wantInterface)

			require.NoError(t, err)
			if !reflect.DeepEqual(gotInterface, wantInterface) {
				t.Errorf("updateCR() = %v, want %v", gotInterface, wantInterface)
			}
		})
	}
}

func Test_updateCR(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		kymaCR     *Kyma
		moduleName string
		channel    string
		want       *Kyma
	}{
		{
			name:       "unchanged modules list",
			moduleName: "module",
			channel:    "",
			kymaCR: &Kyma{
				Spec: KymaSpec{
					Modules: []Module{
						{
							Name: "module",
						},
					},
				},
			},
			want: &Kyma{
				Spec: KymaSpec{
					Modules: []Module{
						{
							Name: "module",
						},
					},
				},
			},
		},
		{
			name:       "added module",
			moduleName: "module",
			channel:    "",
			kymaCR: &Kyma{
				Spec: KymaSpec{
					Modules: []Module{
						{
							Name: "istio",
						},
					},
				},
			},
			want: &Kyma{
				Spec: KymaSpec{
					Modules: []Module{
						{
							Name: "istio",
						},
						{
							Name: "module",
						},
					},
				},
			},
		},
		{
			name:       "added module with channel",
			moduleName: "module",
			channel:    "channel",
			kymaCR: &Kyma{
				Spec: KymaSpec{
					Modules: []Module{
						{
							Name: "istio",
						},
					},
				},
			},
			want: &Kyma{
				Spec: KymaSpec{
					Modules: []Module{
						{
							Name: "istio",
						},
						{
							Name:    "module",
							Channel: "channel",
						},
					},
				},
			},
		},
		{
			name:       "added channel to existing module",
			moduleName: "module",
			channel:    "channel",
			kymaCR: &Kyma{
				Spec: KymaSpec{
					Modules: []Module{
						{
							Name: "module",
						},
					},
				},
			},
			want: &Kyma{
				Spec: KymaSpec{
					Modules: []Module{
						{
							Name:    "module",
							Channel: "channel",
						},
					},
				},
			},
		},
		{
			name:       "removed channel from existing module",
			moduleName: "module",
			channel:    "",
			kymaCR: &Kyma{
				Spec: KymaSpec{
					Modules: []Module{
						{
							Name:    "module",
							Channel: "channel",
						},
					},
				},
			},
			want: &Kyma{
				Spec: KymaSpec{
					Modules: []Module{
						{
							Name: "module",
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		kymaCR := tt.kymaCR
		moduleName := tt.moduleName
		moduleChannel := tt.channel
		want := tt.want
		t.Run(tt.name, func(t *testing.T) {
			got := enableModule(kymaCR, moduleName, moduleChannel)
			gotBytes, err := json.Marshal(got)
			require.NoError(t, err)
			wantBytes, err := json.Marshal(want)
			require.NoError(t, err)
			var gotInterface map[string]interface{}
			var wantInterface map[string]interface{}
			err = json.Unmarshal(gotBytes, &gotInterface)
			require.NoError(t, err)
			err = json.Unmarshal(wantBytes, &wantInterface)
			require.NoError(t, err)
			if !reflect.DeepEqual(gotInterface, wantInterface) {
				t.Errorf("updateCR() = %v, want %v", gotInterface, wantInterface)
			}
		})
	}
}

func fixDefaultKyma() *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "operator.kyma-project.io/v1beta2",
			"kind":       "Kyma",
			"metadata": map[string]interface{}{
				"name":      "default",
				"namespace": "kyma-system",
			},
			"spec": map[string]interface{}{
				"channel": "fast",
				"modules": []interface{}{
					map[string]interface{}{
						"name": "test-module",
					},
				},
			},
		},
	}
}

func Test_client_ListModuleReleaseMeta(t *testing.T) {
	t.Run("list ModuleReleaseMeta", func(t *testing.T) {
		scheme := runtime.NewScheme()
		scheme.AddKnownTypes(GVRModuleReleaseMeta.GroupVersion())
		client := NewClient(dynamic_fake.NewSimpleDynamicClient(scheme,
			fixModuleReleaseMeta("test-1"),
			fixModuleReleaseMeta("test-2"),
		))

		list, err := client.ListModuleReleaseMeta(context.Background())

		require.NoError(t, err)
		require.Len(t, list.Items, 2)
		require.Contains(t, list.Items, fixModuleReleaseMetaStruct("test-1"))
		require.Contains(t, list.Items, fixModuleReleaseMetaStruct("test-2"))

	})
}

func Test_client_ListModuleTemplate(t *testing.T) {
	t.Run("list ModuleReleaseMeta", func(t *testing.T) {
		scheme := runtime.NewScheme()
		scheme.AddKnownTypes(GVRModuleTemplate.GroupVersion())
		client := NewClient(dynamic_fake.NewSimpleDynamicClient(scheme,
			fixModuleTemplate("test-1"),
			fixModuleTemplate("test-2"),
		))

		list, err := client.ListModuleTemplate(context.Background())

		require.NoError(t, err)
		require.Len(t, list.Items, 2)
		require.Contains(t, list.Items, fixModuleTemplateStruct("test-1"))
		require.Contains(t, list.Items, fixModuleTemplateStruct("test-2"))

	})
}

func fixModuleReleaseMetaStruct(moduleName string) ModuleReleaseMeta {
	return ModuleReleaseMeta{
		TypeMeta: v1.TypeMeta{
			APIVersion: "operator.kyma-project.io/v1beta2",
			Kind:       "ModuleReleaseMeta",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      moduleName,
			Namespace: "kyma-system",
		},
		Spec: ModuleReleaseMetaSpec{
			ModuleName: moduleName,
			Channels: []ChannelVersionAssignment{
				{
					Version: "0.1",
					Channel: "regular",
				},
				{
					Version: "0.2",
					Channel: "fast",
				},
			},
		},
	}
}

func fixModuleTemplateStruct(moduleName string) ModuleTemplate {
	return ModuleTemplate{
		TypeMeta: v1.TypeMeta{
			APIVersion: "operator.kyma-project.io/v1beta2",
			Kind:       "ModuleTemplate",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      moduleName,
			Namespace: "kyma-system",
		},
		Spec: ModuleTemplateSpec{
			ModuleName: moduleName,
			Version:    "0.1",
		},
	}
}

func fixModuleReleaseMeta(moduleName string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "operator.kyma-project.io/v1beta2",
			"kind":       "ModuleReleaseMeta",
			"metadata": map[string]interface{}{
				"name":      moduleName,
				"namespace": "kyma-system",
			},
			"spec": map[string]interface{}{
				"moduleName": moduleName,
				"channels": []interface{}{
					map[string]interface{}{
						"version": "0.1",
						"channel": "regular",
					},
					map[string]interface{}{
						"version": "0.2",
						"channel": "fast",
					},
				},
			},
		},
	}
}

func fixModuleTemplate(moduleName string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "operator.kyma-project.io/v1beta2",
			"kind":       "ModuleTemplate",
			"metadata": map[string]interface{}{
				"name":      moduleName,
				"namespace": "kyma-system",
			},
			"spec": map[string]interface{}{
				"moduleName": moduleName,
				"version":    "0.1",
			},
		},
	}
}
