package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/hashicorp/terraform/helper/logging"
	"github.com/hashicorp/terraform/helper/pathorcontents"
	"github.com/hashicorp/terraform/httpclient"
	"github.com/terraform-providers/terraform-provider-google/version"
	"golang.org/x/oauth2"
	googleoauth "golang.org/x/oauth2/google"
	"google.golang.org/api/cloudbilling/v1"
	"google.golang.org/api/cloudresourcemanager/v1"
)

func main() {
	conf := configFromEnv()
	err := conf.LoadAndValidate()
	if err != nil {
		log.Println("Error loading and validating config:", err)
		os.Exit(1)
	}
	fmt.Println("Config successfully loaded ✅")

	fmt.Print("Trying billing API... ")
	for i := 0; i < 5; i++ {
		_, err := conf.clientBilling.BillingAccounts.List().Do()
		if err != nil {
			fmt.Print("‼️  Error listing cloud billing accounts: " + err.Error())
			break
		}
		fmt.Print("✅")
	}
	fmt.Println("")

	fmt.Print("Trying org API... ")
	for i := 0; i < 5; i++ {
		_, err := conf.clientResourceManager.Organizations.Search(&cloudresourcemanager.SearchOrganizationsRequest{}).Do()
		if err != nil {
			fmt.Print("‼️  Error listing organizations: " + err.Error())
			break
		}
		fmt.Print("✅")
	}
	fmt.Println("")
}

type Config struct {
	Credentials string
	AccessToken string
	Scopes      []string

	client    *http.Client
	userAgent string

	tokenSource oauth2.TokenSource

	clientBilling         *cloudbilling.APIService
	clientResourceManager *cloudresourcemanager.Service
}

func configFromEnv() Config {
	var conf Config
	conf.Credentials = os.Getenv("GOOGLE_CREDENTIALS")
	if conf.Credentials == "" {
		conf.Credentials = os.Getenv("GOOGLE_CLOUD_KEYFILE_JSON")
	}
	if conf.Credentials == "" {
		conf.Credentials = os.Getenv("GOOGLE_KEYFILE_JSON")
	}
	conf.AccessToken = os.Getenv("GOOGLE_OAUTH_ACCESS_TOKEN")
	return conf
}

var defaultClientScopes = []string{
	"https://www.googleapis.com/auth/compute",
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/ndev.clouddns.readwrite",
	"https://www.googleapis.com/auth/devstorage.full_control",
}

func (c *Config) LoadAndValidate() error {
	if len(c.Scopes) == 0 {
		c.Scopes = defaultClientScopes
	}

	tokenSource, err := c.getTokenSource(c.Scopes)
	if err != nil {
		return err
	}
	c.tokenSource = tokenSource

	client := oauth2.NewClient(context.Background(), tokenSource)
	client.Transport = logging.NewTransport("Google", client.Transport)
	// Each individual request should return within 30s - timeouts will be retried.
	// This is a timeout for, e.g. a single GET request of an operation - not a
	// timeout for the maximum amount of time a logical request can take.
	client.Timeout, _ = time.ParseDuration("30s")

	terraformVersion := httpclient.UserAgentString()
	providerVersion := fmt.Sprintf("terraform-provider-google/%s", version.ProviderVersion)
	terraformWebsite := "(+https://www.terraform.io)"
	userAgent := fmt.Sprintf("%s %s %s", terraformVersion, terraformWebsite, providerVersion)

	c.client = client
	c.userAgent = userAgent

	log.Printf("[INFO] Instantiating Google Cloud ResourceManager Client...")
	c.clientResourceManager, err = cloudresourcemanager.New(client)
	if err != nil {
		return err
	}
	c.clientResourceManager.UserAgent = userAgent

	log.Printf("[INFO] Instantiating Google Cloud Billing Client...")
	c.clientBilling, err = cloudbilling.New(client)
	if err != nil {
		return err
	}
	c.clientBilling.UserAgent = userAgent

	return nil
}

func (c *Config) getTokenSource(clientScopes []string) (oauth2.TokenSource, error) {
	if c.AccessToken != "" {
		contents, _, err := pathorcontents.Read(c.AccessToken)
		if err != nil {
			return nil, fmt.Errorf("Error loading access token: %s", err)
		}

		log.Printf("[INFO] Authenticating using configured Google JSON 'access_token'...")
		log.Printf("[INFO]   -- Scopes: %s", clientScopes)
		token := &oauth2.Token{AccessToken: contents}
		return oauth2.StaticTokenSource(token), nil
	}

	if c.Credentials != "" {
		contents, _, err := pathorcontents.Read(c.Credentials)
		if err != nil {
			return nil, fmt.Errorf("Error loading credentials: %s", err)
		}

		creds, err := googleoauth.CredentialsFromJSON(context.Background(), []byte(contents), clientScopes...)
		if err != nil {
			return nil, fmt.Errorf("Unable to parse credentials from '%s': %s", contents, err)
		}

		log.Printf("[INFO] Authenticating using configured Google JSON 'credentials'...")
		log.Printf("[INFO]   -- Scopes: %s", clientScopes)
		return creds.TokenSource, nil
	}

	log.Printf("[INFO] Authenticating using DefaultClient...")
	log.Printf("[INFO]   -- Scopes: %s", clientScopes)
	return googleoauth.DefaultTokenSource(context.Background(), clientScopes...)
}
