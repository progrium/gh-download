package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"code.google.com/p/goauth2/oauth"
	"github.com/google/go-github/github"
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

func main() {
	port := os.Getenv("PORT")
	owner := os.Getenv("GITHUB_OWNER")

	t := &oauth.Transport{
		Token: &oauth.Token{AccessToken: os.Getenv("GITHUB_ACCESS_TOKEN")},
	}

	client := github.NewClient(t.Client())

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) < 4 {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		repo := parts[1]
		tag := parts[2]
		filename := strings.Split(parts[3], ".")
		releases, _, err := client.Repositories.ListReleases(owner, repo, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			log.Println("error:", err)
			return
		}
		var release github.RepositoryRelease
		if tag == "latest" {
			release = releases[0]
		} else {
			for _, r := range releases {
				if r.TagName != nil && *r.TagName == tag {
					release = r
					break
				}
			}
		}
		if release.TagName == nil {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		tag = *release.TagName
		version := tag[1:]
		platform := strings.SplitN(strings.ToLower(filename[0]), "_", 2)
		assetFilename := fmt.Sprintf("%s_%s_%s_%s.%s",
			repo, version, platform[0], platform[1], filename[1])
		downloadUrl := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s",
			owner, repo, tag, assetFilename)
		resp, err := http.Get(downloadUrl)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			log.Println("error:", err)
			return
		}
		log.Println("download:", downloadUrl)
		io.Copy(w, resp.Body)
		resp.Body.Close()
	})

	log.Println("serving on port", port, "...")
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
