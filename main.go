package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"time"
)

var (
	token            string
	owner            string
	repository       string
	skipCommentUsers = [...]string{"github-actions[bot]"}
)

const (
	loop                 = 15 /* fetch 100*14 pull request */
	skipCommentsLessThan = 3
	accessDuration       = time.Second * 5
	commentFileName      = "comment.txt"
	prFileName           = "pr.txt"
	reviewFileName       = "year_end_review.md"
)

type PRs []*PR

type Comments []*Comment

type User struct {
	Login string `json:"login"`
}

type PR struct {
	URL      string `json:"url"`
	Number   int64  `json:"number"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	User     `json:"user"`
	Comments `json:"comments"`
}

type Comment struct {
	URL  string `json:"url"`
	Body string `json:"body"`
	User `json:"user"`
}

func init() {
	var errs []error
	if token = os.Getenv("TOKEN"); token == "" {
		errs = append(errs, errors.New("require token"))
	}
	if owner = os.Getenv("OWNER"); owner == "" {
		errs = append(errs, errors.New("require owner"))
	}
	if repository = os.Getenv("REPOSITORY"); repository == "" {
		errs = append(errs, errors.New("require repository"))
	}
	if len(errs) != 0 {
		log.Fatal(errs)
	}
}

func main() {
	var prs PRs
	for i := 1; i < loop; i++ {
		resp, err := prs.Fetch(i)
		if err != nil {
			// NOTE: Although API server rarely returns 502, part of requests successfully decoded, so process continues.
			log.Println(err)
			break
		}
		var tmp PRs
		if err := json.NewDecoder(resp).Decode(&tmp); err != nil {
			log.Println(resp, err)
			continue
		}
		prs = append(prs, tmp...)
		time.Sleep(accessDuration)
	}

	for _, pr := range prs {
		resp, err := pr.FetchComments()
		if err != nil {
			// NOTE: Although API server rarely returns 502, part of requests successfully decoded, so process continues.
			log.Println(err)
			break
		}
		if err := json.NewDecoder(resp).Decode(&pr.Comments); err != nil {
			log.Println(resp, err)
		}
		time.Sleep(accessDuration)
	}

	prsMap := prs.ToMap()

	fp, err := os.OpenFile(reviewFileName, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		log.Fatal(err)
	}
	for user, prs := range prsMap {
		if _, err := fp.WriteString(fmt.Sprintf("## User\n%s\n", user.Login)); err != nil {
			log.Println(err)
		}
		for _, pr := range prs {
			if len(pr.Comments) < skipCommentsLessThan {
				continue
			}
			if _, err := fp.WriteString(fmt.Sprintf(
				"### Title\n%s\n### URL\nhttps://github.com/%s/%s/pull/%d\n### Body\n%s\n#### Comments\n",
				pr.Title,
				owner,
				repository,
				pr.Number,
				strings.ReplaceAll(pr.Body, "#", "#####"), // MEMO: adjust PR's (template) markdown according to overall balance
			)); err != nil {
				log.Println(err)
			}
			for _, comment := range pr.Comments {
				// MEMO: can add skip target
				for _, skipUser := range skipCommentUsers {
					if comment.User.Login == skipUser {
						continue
					}
				}
				if _, err := fp.WriteString(fmt.Sprintf(
					"##### User\n%s\n%s\n",
					comment.User.Login,
					comment.Body,
				)); err != nil {
					log.Println(err)
				}
			}
		}
	}
	if err := fp.Sync(); err != nil {
		log.Println(err)
	}
	if err := fp.Close(); err != nil {
		log.Fatal(err)
	}
}

func (r PRs) Fetch(page int) (io.Reader, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls?state=closed&per_page=100&page=%d", owner, repository, page)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", "application/vnd.github.v3+json")
	req.Header.Add("Authorization", fmt.Sprintf("token %s", token))

	dumpReq, _ := httputil.DumpRequest(req, true)
	fmt.Printf("%s\n", dumpReq)

	client := new(http.Client)
	resp, err := client.Do(req)
	fmt.Println(resp.StatusCode)
	if err != nil {
		return nil, err
	}

	if err := func(resp *http.Response) error {
		dumpResp, _ := httputil.DumpResponse(resp, true)
		fp, err := os.OpenFile(prFileName, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
		if err != nil {
			return err
		}
		defer fp.Close()
		if _, err := fp.Write(dumpResp); err != nil {
			return err
		}
		return nil
	}(resp); err != nil {
		log.Printf("[DEBUG] save dump response error: %v\n", err)
	}
	return resp.Body, nil
}

func (r PRs) ToMap() map[User]PRs {
	m := make(map[User]PRs, len(r))
	for _, v := range r {
		m[v.User] = append(m[v.User], v)
	}
	return m
}

func (r *PR) FetchComments() (io.Reader, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/comments", owner, repository, r.Number)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", "application/vnd.github.v3+json")
	req.Header.Add("Authorization", fmt.Sprintf("token %s", token))

	dumpReq, _ := httputil.DumpRequest(req, true)
	fmt.Printf("%s\n", dumpReq)

	client := new(http.Client)
	resp, err := client.Do(req)
	fmt.Println(resp.StatusCode)
	if err != nil {
		return nil, err
	}

	if err := func(resp *http.Response) error {
		dumpResp, _ := httputil.DumpResponse(resp, true)
		fp, err := os.OpenFile(commentFileName, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
		if err != nil {
			return err
		}
		defer fp.Close()
		if _, err := fp.Write(dumpResp); err != nil {
			return err
		}
		return nil
	}(resp); err != nil {
		log.Printf("[DEBUG] save dump response error: %v\n", err)
	}
	return resp.Body, nil
}
