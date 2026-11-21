package contracts

import "testing"

func TestDetectS3Region(t *testing.T) {
	cases := []struct {
		endpoint string
		want     string
	}{
		// Hetzner
		{"fsn1.your-objectstorage.com", "fsn1"},
		{"hel1.your-objectstorage.com", "hel1"},
		{"nbg1.your-objectstorage.com", "nbg1"},
		{"ash.your-objectstorage.com", "ash"},
		{"sin.your-objectstorage.com", "sin"},
		{"https://fsn1.your-objectstorage.com/", "fsn1"},
		{"https://fsn1.your-objectstorage.com:443/", "fsn1"},
		{"mybucket.hel1.your-objectstorage.com", "hel1"},
		{"HTTP://FSN1.YOUR-OBJECTSTORAGE.COM", "fsn1"},

		// DigitalOcean
		{"ams3.digitaloceanspaces.com", "ams3"},
		{"mybucket.nyc3.digitaloceanspaces.com", "nyc3"},

		// Linode
		{"eu-central-1.linodeobjects.com", "eu-central-1"},

		// Wasabi
		{"s3.eu-central-1.wasabisys.com", "eu-central-1"},

		// Scaleway
		{"s3.fr-par.scw.cloud", "fr-par"},

		// Backblaze
		{"s3.us-west-004.backblazeb2.com", "us-west-004"},

		// AWS
		{"s3.eu-west-1.amazonaws.com", "eu-west-1"},
		{"s3-us-west-2.amazonaws.com", "us-west-2"},
		{"mybucket.s3.eu-central-1.amazonaws.com", "eu-central-1"},

		// No match o endpoint local
		{"localhost:9000", ""},
		{"127.0.0.1:9000", ""},
		{"192.168.1.100:9000", ""},
		{"minio.internal", ""},
		{"", ""},
	}

	for _, c := range cases {
		got := DetectS3Region(c.endpoint)
		if got != c.want {
			t.Errorf("DetectS3Region(%q) = %q, want %q", c.endpoint, got, c.want)
		}
	}
}
