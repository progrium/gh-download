package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"code.google.com/p/goauth2/oauth"
	"github.com/google/go-github/github"
	"github.com/gorilla/mux"
	"github.com/inconshreveable/go-keen"
)

func assert(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func marshal(obj interface{}) []byte {
	bytes, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		log.Println("marshal:", err)
	}
	return bytes
}

func normalVersion(version string) string {
	if version[0] == 'v' {
		return version[1:]
	}
	return version
}

func expandVersion(releases []github.RepositoryRelease, version string) string {
	var release github.RepositoryRelease
	if version == "latest" {
		release = releases[0]
	} else {
		for _, r := range releases {
			if r.TagName != nil && *r.TagName == version {
				release = r
				break
			}
		}
	}
	if release.TagName == nil {
		return ""
	}
	return *release.TagName
}

func proxyDownload(w http.ResponseWriter, url string) {
	resp, err := http.Get(url)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		log.Println("error:", err)
		return
	}
	if resp.StatusCode == 200 {
		log.Println("download:", url)
	} else {
		log.Println("error:", resp.Status, url)
	}
	w.Header().Set("Backend-Url", url)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
	resp.Body.Close()
}

type DownloadEvent struct {
	Repo            string
	Tag             string
	ExpandedVersion string
	Extension       string
	Platform        string
	ClientAddress   string
}

func main() {
	port := os.Getenv("PORT")
	owner := os.Getenv("GITHUB_OWNER")

	t := &oauth.Transport{
		Token: &oauth.Token{AccessToken: os.Getenv("GITHUB_ACCESS_TOKEN")},
	}

	client := github.NewClient(t.Client())

	keenProject := os.Getenv("KEEN_PROJECT")
	keenWriteKey := os.Getenv("KEEN_WRITE_KEY")
	keenFlushInterval := 1 * time.Second

	keenClient := &keen.Client{WriteKey: keenWriteKey, ProjectID: keenProject}
	keenBatchClient := keen.NewBatchClient(keenClient, keenFlushInterval)

	r := mux.NewRouter()

	r.HandleFunc("/{repo}/latest/version.txt", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		releases, _, err := client.Repositories.ListReleases(owner, vars["repo"], nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			log.Println("error:", err)
			return
		}
		io.WriteString(w, normalVersion(expandVersion(releases, "latest"))+"\n")
	})

	handleDownload := func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)

		repo := vars["repo"]

		releases, _, err := client.Repositories.ListReleases(owner, repo, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			log.Println("error:", err)
			return
		}

		version := expandVersion(releases, vars["tag"])
		w.Header().Set("Version", version)
		if version == "" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		var filenameParts []string

		platform, ok := vars["platform"]
		if ok {
			w.Header().Set("Platform", platform)
			platform = strings.ToLower(platform)
			if strings.Count(platform, "_") < 1 {
				http.Error(w, "Not found", http.StatusNotFound)
				return
			}

			filenameParts = []string{repo, normalVersion(version), platform}
		} else {
			filenameParts = []string{repo, version}
		}

		if clientAddr, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			keenBatchClient.AddEvent("downloads", &DownloadEvent{
				Repo:            repo,
				Tag:             vars["tag"],
				ExpandedVersion: version,
				Extension:       vars["ext"],
				Platform:        platform,
				ClientAddress:   clientAddr,
			})
		}

		proxyDownload(w, fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s.%s",
			owner,
			repo,
			version,
			strings.Join(filenameParts, "_"),
			vars["ext"]))
	}

	r.HandleFunc("/{repo}/{tag}.{ext}", handleDownload)
	r.HandleFunc("/{repo}/{tag}/{platform}.{ext}", handleDownload)

	http.Handle("/", r)
	log.Println("serving on port", port, "for", owner, "...")
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
