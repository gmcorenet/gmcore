package manifest

import "testing"

func TestEffectiveSDKsStandardIncludesBaseline(t *testing.T) {
	m := &Manifest{
		Framework: Component{Repo: "gmcorenet/framework", Release: "v2.0.0", Path: "vendor/framework"},
		SDKs: []SDKComponent{
			{Name: "gmcore-router", Release: "v1.2.3"},
		},
	}

	sdks := m.EffectiveSDKs("myapp")
	requireSDK(t, sdks, "gmcore-router", "v1.2.3")
	requireSDK(t, sdks, "gmcore-transport", "v1.2.3")
	requireSDK(t, sdks, "gmcore-lifecycle", "v1.2.3")
	requireSDK(t, sdks, "gmcore-log", "v1.2.3")
	requireSDK(t, sdks, "gmcore-security", "v1.2.3")
	requireSDK(t, sdks, "gmcore-ratelimit", "v1.2.3")
}

func TestEffectiveSDKsGatewayAddsGatewaySet(t *testing.T) {
	m := &Manifest{
		Framework: Component{Repo: "gmcorenet/framework", Release: "v3.1.0", Path: "vendor/framework"},
	}

	sdks := m.EffectiveSDKs("gateway")
	requireSDK(t, sdks, "gmcore-router", "v3.1.0")
	requireSDK(t, sdks, "gmcore-events", "v3.1.0")
	requireSDK(t, sdks, "gmcore-validation", "v3.1.0")
	requireSDK(t, sdks, "gmcore-transport", "v3.1.0")
	requireSDK(t, sdks, "gmcore-lifecycle", "v3.1.0")
}

func TestEffectiveSDKsNoDuplicateAndPreservesExistingRelease(t *testing.T) {
	m := &Manifest{
		Framework: Component{Repo: "gmcorenet/framework", Release: "v9.0.0", Path: "vendor/framework"},
		SDKs: []SDKComponent{
			{Name: "gmcore-transport", Release: "v5.4.3"},
		},
	}

	sdks := m.EffectiveSDKs("myapp")
	count := 0
	for _, sdk := range sdks {
		if sdk.Name == "gmcore-transport" {
			count++
			if sdk.Release != "v5.4.3" {
				t.Fatalf("unexpected release for gmcore-transport: %s", sdk.Release)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected one gmcore-transport entry, got %d", count)
	}
}

func requireSDK(t *testing.T, sdks []SDKComponent, name, release string) {
	t.Helper()
	for _, sdk := range sdks {
		if sdk.Name == name {
			if sdk.Release != release {
				t.Fatalf("sdk %s has release %s, want %s", name, sdk.Release, release)
			}
			return
		}
	}
	t.Fatalf("sdk %s not found", name)
}
