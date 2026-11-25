package config

import (
	"encoding/base64"
	"net/url"
	"strings"
	"time"
)

type DeliveryConfig struct {
	MediaEnabled, PushEnabled, PathStyle                                    bool
	Endpoint, Region, Bucket, PublicURL, Prefix, AccessKey, CredentialsFile string
	SecretKey, TokenKey                                                     Secret
	AnimationHosts                                                          []string
	Timeout, DatabaseTimeout                                                time.Duration
	Workers                                                                 int
}

func LoadDelivery(l LookupEnv, env Environment) (DeliveryConfig, error) {
	if l == nil {
		return DeliveryConfig{}, invalid("DELIVERY", "lookup required")
	}
	c := DeliveryConfig{Region: "us-east-1", Prefix: string(env), Timeout: 5 * time.Second, Workers: 2, PathStyle: true}
	var e error
	parseBool := func(l LookupEnv, k, def string) (bool, error) {
		v, ok := l(k)
		if !ok {
			v = def
		}
		if v == "true" {
			return true, nil
		}
		if v == "false" {
			return false, nil
		}
		return false, invalid(k, "must be true or false")
	}
	c.MediaEnabled, e = parseBool(l, "MEDIA_ENABLED", "false")
	if e != nil {
		return c, e
	}
	c.PushEnabled, e = parseBool(l, "PUSH_ENABLED", "false")
	if e != nil {
		return c, e
	}
	c.PathStyle, e = parseBool(l, "MEDIA_PATH_STYLE", "true")
	if e != nil {
		return c, e
	}
	read := func(k, def string) string {
		v, ok := l(k)
		if !ok {
			return def
		}
		return v
	}
	c.Endpoint = read("MEDIA_ENDPOINT", "")
	c.Region = read("MEDIA_REGION", c.Region)
	c.Bucket = read("MEDIA_BUCKET", "")
	c.PublicURL = strings.TrimSuffix(read("MEDIA_PUBLIC_URL", ""), "/")
	c.Prefix = read("MEDIA_PREFIX", c.Prefix)
	c.AccessKey = read("MEDIA_ACCESS_KEY", "")
	c.SecretKey = Secret(read("MEDIA_SECRET_KEY", ""))
	c.CredentialsFile = read("FCM_CREDENTIALS_FILE", "")
	c.TokenKey = Secret(read("PUSH_TOKEN_ENCRYPTION_KEY", ""))
	if !validName(c.Prefix) || strings.Contains(c.Prefix, ".") {
		return c, invalid("MEDIA_PREFIX", "must be a single environment-specific name")
	}
	https := func(raw string) bool {
		u, e := url.Parse(raw)
		return len(raw) <= 256 && e == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == ""
	}
	if c.MediaEnabled && (!https(c.Endpoint) || !https(c.PublicURL) || c.Bucket == "" || c.AccessKey == "" || c.SecretKey.Value() == "" || c.Region == "") {
		return c, invalid("MEDIA_ENABLED", "requires explicit verified HTTPS endpoints, region, bucket and credentials")
	}
	if c.PushEnabled && c.CredentialsFile == "" {
		return c, invalid("FCM_CREDENTIALS_FILE", "required when push is enabled")
	}
	if c.TokenKey.Value() != "" {
		b, err := base64.StdEncoding.DecodeString(c.TokenKey.Value())
		if err != nil || len(b) != 32 {
			return c, invalid("PUSH_TOKEN_ENCRYPTION_KEY", "must encode 32 bytes")
		}
	} else if c.PushEnabled {
		return c, invalid("PUSH_TOKEN_ENCRYPTION_KEY", "required when push is enabled")
	}
	hosts := read("GIFT_ANIMATION_HOSTS", "")
	if len(hosts) > 4096 {
		return c, invalid("GIFT_ANIMATION_HOSTS", "at most 4096 bytes")
	}
	if hosts != "" {
		for _, h := range strings.Split(hosts, ",") {
			if !https("https://"+h) || strings.ContainsAny(h, "/:@ ") {
				return c, invalid("GIFT_ANIMATION_HOSTS", "must contain HTTPS hostnames")
			}
			c.AnimationHosts = append(c.AnimationHosts, h)
		}
	}
	n, e := integer(l, "DELIVERY_WORKERS", "2", 1, 4)
	if e != nil {
		return c, e
	}
	c.Workers = int(n)
	c.DatabaseTimeout, e = time.ParseDuration(read("DELIVERY_DATABASE_TIMEOUT", "8s"))
	if e != nil || c.DatabaseTimeout < 100*time.Millisecond || c.DatabaseTimeout > 30*time.Second {
		return c, invalid("DELIVERY_DATABASE_TIMEOUT", "must be 100ms to 30s")
	}
	return c, nil
}
