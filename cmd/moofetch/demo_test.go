package main

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/sebasinmas/MooFetch/internal/plugins/demo"
	"github.com/sebasinmas/MooFetch/internal/tui"
)

func TestWithDemo_SimulatesDetectionAndScan(t *testing.T) {
	opts := withDemo(&tui.AuthOptions{})
	if !opts.Demo {
		t.Fatal("Demo flag must be set")
	}

	cookie, err := opts.Detect(context.Background(), "campusvirtual.ufro.cl")
	if err != nil || cookie != demoCookie {
		t.Fatalf("Detect = %q, %v", cookie, err)
	}

	found, err := opts.Scan(context.Background(), "https://campusvirtual.ufro.cl/course/view.php?id=1", cookie)
	if err != nil || len(found) != len(demoResources) {
		t.Fatalf("Scan = %d resources, %v", len(found), err)
	}
	// the caller may not mutate the fixture through the returned slice
	found[0].Title = "mutado"
	if demoResources[0].Title == "mutado" {
		t.Error("Scan must return a copy")
	}
}

func TestDemoResources_AreValidAndDistinct(t *testing.T) {
	titles, urls, files := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for i, r := range demoResources {
		if r.Title == "" || r.URL == "" {
			t.Fatalf("resource %d is incomplete: %+v", i, r)
		}
		if u, err := url.Parse(r.URL); err != nil || u.Host != "campusvirtual.ufro.cl" {
			t.Errorf("resource %d has a bad URL %q", i, r.URL)
		}
		name := demo.ExtractPDFResource(r.URL, i+1)
		if len(name) < 5 || name[len(name)-4:] != ".pdf" {
			t.Errorf("resource %d: demo downloader would name it %q", i, name)
		}
		if titles[r.Title] || urls[r.URL] || files[name] {
			t.Errorf("resource %d duplicates another one", i)
		}
		titles[r.Title], urls[r.URL], files[name] = true, true, true
	}
}

func TestWithDemo_RespectsCancellation(t *testing.T) {
	opts := withDemo(&tui.AuthOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	if _, err := opts.Detect(ctx, "x.cl"); !errors.Is(err, context.Canceled) {
		t.Errorf("Detect err = %v, want context.Canceled", err)
	}
	if _, err := opts.Scan(ctx, "https://x.cl", ""); !errors.Is(err, context.Canceled) {
		t.Errorf("Scan err = %v, want context.Canceled", err)
	}
	if time.Since(start) > time.Second {
		t.Error("cancelled demo steps must return immediately")
	}
}

func TestAuthOptions_DemoFlagEnablesSimulation(t *testing.T) {
	defer func(old bool) { flagDemo = old }(flagDemo)
	flagDemo = true
	opts, err := authOptions()
	if err != nil || !opts.Demo {
		t.Fatalf("demo options = %+v, %v", opts, err)
	}
	flagDemo = false
	opts, err = authOptions()
	if err != nil || opts.Demo {
		t.Fatalf("normal options must not be demo: %+v, %v", opts, err)
	}
}
