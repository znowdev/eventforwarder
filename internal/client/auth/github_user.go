package auth

import (
	"encoding/json"
	"github.com/gogama/httpx/request"
	"github.com/mscno/zerrors"
	"net/http"
	"time"
)

type GithubProvider func(accessToken string) (GitHubUser, error)

func GetGitHubUser(accessToken string) (GitHubUser, error) {
	var user GitHubUser
	r, err := request.NewPlan("GET", "https://api.github.com/user", nil)
	if err != nil {
		return user, err
	}
	r.Header.Set("Authorization", "Bearer "+accessToken)

	result, err := httpClient.Do(r)
	if err != nil {
		return user, err
	}
	if result.StatusCode() != http.StatusOK {
		return user, zerrors.Internal("failed to get github user", "status_code", result.StatusCode())
	}

	if err := json.Unmarshal(result.Body, &user); err != nil {
		return user, zerrors.ToInternal(err, "failed to unmarshal github user")
	}

	return user, nil
}

type GitHubUser struct {
	Login             string    `json:"login"`
	ID                int       `json:"id"`
	NodeID            string    `json:"node_id"`
	AvatarURL         string    `json:"avatar_url"`
	GravatarID        string    `json:"gravatar_id"`
	URL               string    `json:"url"`
	HTMLURL           string    `json:"html_url"`
	FollowersURL      string    `json:"followers_url"`
	FollowingURL      string    `json:"following_url"`
	GistsURL          string    `json:"gists_url"`
	StarredURL        string    `json:"starred_url"`
	SubscriptionsURL  string    `json:"subscriptions_url"`
	OrganizationsURL  string    `json:"organizations_url"`
	ReposURL          string    `json:"repos_url"`
	EventsURL         string    `json:"events_url"`
	ReceivedEventsURL string    `json:"received_events_url"`
	Type              string    `json:"type"`
	SiteAdmin         bool      `json:"site_admin"`
	Name              string    `json:"name"`
	Company           any       `json:"company"`
	Blog              string    `json:"blog"`
	Location          any       `json:"location"`
	Email             any       `json:"email"`
	Hireable          any       `json:"hireable"`
	Bio               string    `json:"bio"`
	TwitterUsername   any       `json:"twitter_username"`
	PublicRepos       int       `json:"public_repos"`
	PublicGists       int       `json:"public_gists"`
	Followers         int       `json:"followers"`
	Following         int       `json:"following"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}
