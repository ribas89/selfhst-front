package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanGroupsAllAvailableVariations(t *testing.T) {
	root := t.TempDir()
	for _, file := range []string{
		"svg/immich.svg", "svg/immich-dark.svg", "svg/immich-light.svg",
		"png/immich.png", "webp/immich.webp", "avif/immich.avif", "ico/immich.ico",
		"png/missing-svg.png", "unrelated/example.png",
	} {
		dest := filepath.Join(root, file)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, []byte("fixture"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	metadata := `[["Immich", "immich", "Y", "Y", "Y", "Y", "Y", "Self-Hosted", "", "1"]]`
	if err := os.WriteFile(filepath.Join(root, "index-consolidated.json"), []byte(metadata), 0644); err != nil {
		t.Fatal(err)
	}
	cat, err := scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(cat.Items), 2; got != want {
		t.Fatalf("got %d icons, want %d", got, want)
	}
	for _, icon := range cat.Items {
		if icon.Slug == "immich" {
			if icon.Name != "Immich" || icon.Category != "Self-Hosted" {
				t.Errorf("metadata not loaded: %+v", icon)
			}
			if len(icon.Variants) != 7 {
				t.Errorf("expected 7 variations, got %d", len(icon.Variants))
			}
			if icon.Variants[0].URL != "/immich.svg" {
				t.Errorf("unexpected first variation: %+v", icon.Variants[0])
			}
			return
		}
	}
	t.Fatal("immich not found")
}

func TestScanNestedCheckout(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "repo", "svg")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "server.svg"), []byte("svg"), 0644); err != nil {
		t.Fatal(err)
	}
	cat, err := scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Items) != 1 {
		t.Fatalf("got %d icons; wanted 1", len(cat.Items))
	}
}

func TestScanUsesUpstreamCreatedAt(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"earlier", "later", "unindexed"} {
		file := filepath.Join(root, "png", name+".png")
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil { t.Fatal(err) }
		if err := os.WriteFile(file, []byte("fixture"), 0644); err != nil { t.Fatal(err) }
	}
	index := `[
      {"Name":"Earlier","Reference":"earlier","Category":"Other","CreatedAt":"2024-08-16 00:27:23+00:00"},
      {"Name":"Later","Reference":"later","Category":"Other","CreatedAt":"2026-10-26 21:02:00+00:00"}
    ]`
	if err := os.WriteFile(filepath.Join(root,"index.json"), []byte(index), 0644); err != nil {t.Fatal(err)}
	cat, err := scan(root)
	if err != nil {t.Fatal(err)}
	if len(cat.Items) != 3 {t.Fatalf("got %d items",len(cat.Items))}
	got := map[string]string{}
	for _, icon := range cat.Items {got[icon.Slug] = icon.CreatedAt}
	if got["earlier"] != "2024-08-16T00:27:23Z" || got["later"] != "2026-10-26T21:02:00Z" || got["unindexed"] != "" {
		t.Fatalf("incorrect upstream timestamps: %+v",got)
	}
}
