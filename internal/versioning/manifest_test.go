package versioning

import "testing"

func TestCanonicalHashStableAcrossJSONFieldOrder(t *testing.T) {
	a := []byte(`{"b":2,"a":{"y":1,"x":[3,2,1]}}`)
	b := []byte(`{
		"a": {"x": [3,2,1], "y": 1},
		"b": 2
	}`)

	ha, err := CanonicalJSONHash(a)
	if err != nil {
		t.Fatal(err)
	}
	hb, err := CanonicalJSONHash(b)
	if err != nil {
		t.Fatal(err)
	}
	if ha != hb {
		t.Fatalf("hash mismatch: %s != %s", ha, hb)
	}
}

func TestPromptAssetHashChangesWithContent(t *testing.T) {
	a := PromptAsset{ID: "decide", Version: "v1", Content: "hello"}
	b := PromptAsset{ID: "decide", Version: "v1", Content: "hello!"}

	if a.Hash() == b.Hash() {
		t.Fatal("hash should change when prompt content changes")
	}
}
