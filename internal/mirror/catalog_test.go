package mirror

import "testing"

func TestExpandShortName(t *testing.T) {
	if got := Expand("1ms"); got != "docker.1ms.run" {
		t.Fatalf("Expand(1ms) = %s", got)
	}
	if got := Expand("https://docker.xuanyuan.me/"); got != "docker.xuanyuan.me" {
		t.Fatalf("Expand(url) = %s", got)
	}
	if got := Expand("xuanyuan"); got != "docker.xuanyuan.me" {
		t.Fatalf("Expand(xuanyuan) = %s", got)
	}
}

func TestExpandKeepsExplicitPrefix(t *testing.T) {
	got := Expand("swr.example.com/ddn-k8s/docker.io")
	if got != "swr.example.com/ddn-k8s/docker.io" {
		t.Fatalf("Expand = %s", got)
	}
}

func TestByTierOrder(t *testing.T) {
	groups := ByTier()
	if len(groups) != 2 || len(groups[0]) != 2 || len(groups[1]) != 3 {
		t.Fatalf("tiers = %#v", groups)
	}
	if groups[0][0].Host != "docker.1ms.run" || groups[0][1].Host != "docker.xuanyuan.me" {
		t.Fatalf("primary = %#v", groups[0])
	}
	if groups[1][0].Host != "docker.m.daocloud.io" {
		t.Fatalf("backup starts with %s", groups[1][0].Host)
	}
}

func TestIsDockerHub(t *testing.T) {
	if !IsDockerHub("index.docker.io") || !IsDockerHub("docker.io") {
		t.Fatal("docker hub not recognized")
	}
	if IsDockerHub("ghcr.io") {
		t.Fatal("ghcr.io is not docker hub")
	}
}
