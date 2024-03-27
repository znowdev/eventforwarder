package auth

import (
	"context"
	"fmt"
	"github.com/gogama/httpx"
	"github.com/gogama/httpx/retry"
	"github.com/gogama/httpx/timeout"
	"github.com/mscno/zerrors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

//func Login(ctx context.Context, githubClientId string) (*api.AccessToken, error) {
//	flow := &oauth.Flow{
//		Host:     oauth.GitHubHost("https://github.com"),
//		ClientID: os.Getenv(githubClientId),
//		//ClientSecret: os.Getenv("OAUTH_CLIENT_SECRET"), // only applicable to web app flow
//		//CallbackURI:  "http://127.0.0.1:8888/callback",      // only applicable to web app flow
//		Scopes: []string{"read:user"},
//	}
//
//	return flow.DeviceFlow()
//
//}

func defaultClient() *httpx.Client {
	return &httpx.Client{
		TimeoutPolicy: timeout.Fixed(10 * time.Second),
		RetryPolicy: retry.NewPolicy(
			retry.Times(10).And(retry.StatusCode(422, 501, 502, 504).Or(retry.TransientErr)),
			retry.NewExpWaiter(500*time.Millisecond, 30*time.Second, time.Now()),
		),
	}
}

var httpClient = defaultClient()

func Login(ctx context.Context, githubClientId string) (*AccessTokenResponse, error) {

	v := url.Values{
		"scope":     {"read:user"},
		"client_id": {githubClientId},
	}

	result, err := httpClient.PostForm("https://github.com/login/device/code", v)
	if err != nil {
		return nil, err
	}
	if result.StatusCode() != http.StatusOK {
		return nil, err
	}

	q, err := url.ParseQuery(string(result.Body))
	if err != nil {
		return nil, err
	}

	fmt.Println("Please visit", q.Get("verification_uri"), "and enter the following code:")
	fmt.Println("User code:", q.Get("user_code"))

	intervalSeconds, err := strconv.Atoi(q.Get("interval"))
	if err != nil {
		return nil, fmt.Errorf("could not parse interval=%q as integer: %w", q.Get("interval"), err)
	}

	expiresIn, err := strconv.Atoi(q.Get("expires_in"))
	if err != nil {
		return nil, fmt.Errorf("could not parse expires_in=%q as integer: %w", q.Get("expires_in"), err)
	}

	return Wait(ctx, "https://github.com/login/oauth/access_token", WaitOptions{
		ClientID:     githubClientId,
		ClientSecret: "",
		DeviceCode: &CodeResponse{
			UserCode:                q.Get("user_code"),
			VerificationURI:         q.Get("verification_uri"),
			VerificationURIComplete: q.Get("verification_uri_complete"),
			DeviceCode:              q.Get("device_code"),
			ExpiresIn:               expiresIn,
			Interval:                intervalSeconds,
		},
		GrantType: defaultGrantType,
		newPoller: nil,
	})
}

type AccessTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

// WaitOptions specifies parameters to poll the server with until authentication completes.
type WaitOptions struct {
	// ClientID is the app client ID value.
	ClientID string
	// ClientSecret is the app client secret value. Optional: only pass if the server requires it.
	ClientSecret string
	// DeviceCode is the value obtained from RequestCode.
	DeviceCode *CodeResponse
	// GrantType overrides the default value specified by OAuth 2.0 Device Code. Optional.
	GrantType string

	newPoller pollerFactory
}

// CodeResponse holds information about the authorization-in-progress.
type CodeResponse struct {
	// The user verification code is displayed on the device so the user can enter the code in a browser.
	UserCode string
	// The verification URL where users need to enter the UserCode.
	VerificationURI string
	// The optional verification URL that includes the UserCode.
	VerificationURIComplete string

	// The device verification code is 40 characters and used to verify the device.
	DeviceCode string
	// The number of seconds before the DeviceCode and UserCode expire.
	ExpiresIn int
	// The minimum number of seconds that must pass before you can make a new access token request to
	// complete the device authorization.
	Interval int
}

const defaultGrantType = "urn:ietf:params:oauth:grant-type:device_code"

// Error is the result of an unexpected HTTP response from the server.
type ApiError struct {
	Code         string `json:"code"`
	ResponseCode int    `json:"response_code"`
	RequestURI   string `json:"request_uri"`
	Message      string `json:"message"`
}

// Wait polls the server at uri until authorization completes.
func Wait(ctx context.Context, uri string, opts WaitOptions) (*AccessTokenResponse, error) {
	slog.Debug("polling uri for code", "uri", uri)
	checkInterval := time.Duration(opts.DeviceCode.Interval) * time.Second
	expiresIn := time.Duration(opts.DeviceCode.ExpiresIn) * time.Second
	grantType := opts.GrantType
	if opts.GrantType == "" {
		grantType = defaultGrantType
	}

	makePoller := opts.newPoller
	if makePoller == nil {
		makePoller = newPoller
	}
	_, poll := makePoller(ctx, checkInterval, expiresIn)
	slog.Debug("waiting for authorization...")

	for {
		if err := poll.Wait(); err != nil {
			return nil, zerrors.ToInternal(err, "polling error")
		}

		values := url.Values{
			"client_id":   {opts.ClientID},
			"device_code": {opts.DeviceCode.DeviceCode},
			"grant_type":  {grantType},
		}

		// Google's "OAuth 2.0 for TV and Limited-Input Device Applications" requires `client_secret`.
		if opts.ClientSecret != "" {
			values.Add("client_secret", opts.ClientSecret)
		}

		resp, err := httpClient.PostForm(uri, values)
		if err != nil {
			return nil, zerrors.ToInternal(err, "error executing wait request")
		}

		q, err := url.ParseQuery(string(resp.Body))
		if err != nil {
			return nil, err
		}

		if resp.StatusCode() != http.StatusOK {
			return nil, zerrors.Internal("unexpected response", "response_code", resp.StatusCode(), "error", string(resp.Body))
		}

		apiError := ApiError{
			Code:         q.Get("error"),
			ResponseCode: resp.StatusCode(),
			RequestURI:   q.Get("error_uri"),
			Message:      q.Get("error_description"),
		}

		if apiError.Code != "" {
			if apiError.Code == "authorization_pending" {
				continue
			}
			return nil, zerrors.Internal("unexpected response", "api_error", apiError)
		}

		return &AccessTokenResponse{
			AccessToken:  q.Get("access_token"),
			RefreshToken: q.Get("refresh_token"),
			TokenType:    q.Get("token_type"),
			Scope:        q.Get("scope"),
		}, nil

	}
}
