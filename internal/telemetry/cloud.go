package telemetry

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"
)

type cloudCandidate struct {
	name    string
	url     string
	headers map[string]string
	match   func(*http.Response) bool
}

var cloudCandidates = []cloudCandidate{
	{
		name: "aws", url: "http://169.254.169.254/latest/meta-data/",
		match: func(r *http.Response) bool {
			return r.StatusCode == http.StatusOK || (r.StatusCode == http.StatusUnauthorized && strings.HasPrefix(r.Header.Get("Server"), "EC2ws"))
		},
	},
	{
		name: "gcp", url: "http://169.254.169.254/computeMetadata/v1/",
		headers: map[string]string{"Metadata-Flavor": "Google"},
		match: func(r *http.Response) bool {
			return r.StatusCode == http.StatusOK && r.Header.Get("Metadata-Flavor") == "Google"
		},
	},
	{
		name: "azure", url: "http://169.254.169.254/metadata/instance?api-version=2021-02-01",
		headers: map[string]string{"Metadata": "true"},
		match: func(r *http.Response) bool {
			return r.StatusCode == http.StatusOK
		},
	},
}

func probeCloud() string {
	if v := dmiVendor(); v != "" {
		return v
	}
	return probeIMDS()
}

func dmiVendor() string {
	var vendor strings.Builder
	for _, f := range []string{"sys_vendor", "product_name"} {
		if b, err := os.ReadFile("/sys/class/dmi/id/" + f); err == nil {
			vendor.Write(b)
		}
	}
	v := strings.ToLower(vendor.String())
	switch {
	case strings.Contains(v, "amazon"):
		return "aws"
	case strings.Contains(v, "google"):
		return "gcp"
	case strings.Contains(v, "microsoft"):
		return "azure"
	}
	return ""
}

func probeIMDS() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	winner := make(chan string, len(cloudCandidates))
	client := &http.Client{Timeout: 2 * time.Second}

	for _, c := range cloudCandidates {
		c := c
		go func() {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
			if err != nil {
				return
			}
			for k, v := range c.headers {
				req.Header.Set(k, v)
			}
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			if c.match(resp) {
				winner <- c.name
			}
		}()
	}

	select {
	case cloud := <-winner:
		return cloud
	case <-ctx.Done():
		return "unknown"
	}
}

func detectPlatform() string {
	switch {
	case os.Getenv("KUBERNETES_SERVICE_HOST") != "":
		return "kubernetes"
	case os.Getenv("ECS_CONTAINER_METADATA_URI_V4") != "":
		return "ecs"
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "docker"
	}
	return "binary"
}
