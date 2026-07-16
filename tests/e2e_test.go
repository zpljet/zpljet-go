package tests

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zpljet/zpljet-go"
)

const e2eZPL = "^XA^FO50,50^A0N,50,50^FDZPLJet e2e^FS^XZ"

func e2eClient(t *testing.T, apiKey string) *zpljet.Client {
	t.Helper()
	if apiKey == "" {
		apiKey = os.Getenv("ZPLJET_API_KEY")
	}
	if os.Getenv("ZPLJET_API_KEY") == "" {
		t.Skip("ZPLJET_API_KEY not set")
	}
	opts := []zpljet.Option{}
	if baseURL := os.Getenv("ZPLJET_BASE_URL"); baseURL != "" {
		opts = append(opts, zpljet.WithBaseURL(baseURL))
	}
	client, err := zpljet.New(apiKey, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestE2EConvertsZPLToPDF(t *testing.T) {
	client := e2eClient(t, "")
	label, err := client.Convert(context.Background(), zpljet.ConvertRequest{ZPL: e2eZPL})
	if err != nil {
		t.Fatal(err)
	}
	if label.ContentType != "application/pdf" || label.ID == "" {
		t.Errorf("ContentType=%q ID=%q", label.ContentType, label.ID)
	}
	if !bytes.HasPrefix(label.Data, []byte("%PDF")) {
		t.Errorf("Data starts with %q", label.Data[:4])
	}
}

func TestE2EConvertsZPLToPNG(t *testing.T) {
	client := e2eClient(t, "")
	label, err := client.Convert(context.Background(),
		zpljet.ConvertRequest{ZPL: e2eZPL, Format: zpljet.FormatPNG, DPMM: 12})
	if err != nil {
		t.Fatal(err)
	}
	if label.ContentType != "image/png" {
		t.Errorf("ContentType = %q", label.ContentType)
	}
	if !bytes.HasPrefix(label.Data, []byte{0x89, 'P', 'N', 'G'}) {
		t.Errorf("Data starts with %q", label.Data[:4])
	}
}

func TestE2ERejectsInvalidZPL(t *testing.T) {
	client := e2eClient(t, "")
	_, err := client.Convert(context.Background(), zpljet.ConvertRequest{ZPL: "not zpl at all"})
	var apiErr *zpljet.Error
	if !errors.As(err, &apiErr) || apiErr.Code != zpljet.CodeInvalidRequest || apiErr.Param != "zpl" {
		t.Fatalf("err = %v", err)
	}
}

func TestE2ERejectsBadAPIKey(t *testing.T) {
	client := e2eClient(t, "zpl_definitely_not_a_real_key")
	_, err := client.Convert(context.Background(), zpljet.ConvertRequest{ZPL: e2eZPL})
	var apiErr *zpljet.Error
	if !errors.As(err, &apiErr) || apiErr.Code != zpljet.CodeInvalidAPIKey {
		t.Fatalf("err = %v", err)
	}
}

func TestE2EHostsFileOrCleanlyRefusesOnFreePlan(t *testing.T) {
	client := e2eClient(t, "")
	hosted, err := client.ConvertToURL(context.Background(), zpljet.ConvertRequest{ZPL: e2eZPL})
	if err != nil {
		var apiErr *zpljet.Error
		if errors.As(err, &apiErr) &&
			(apiErr.Code == zpljet.CodeHostingNotAllowed || apiErr.Code == zpljet.CodeNoRetentionEnforced) {
			return
		}
		t.Fatal(err)
	}
	if !strings.HasPrefix(hosted.URL, "http") || hosted.Pages < 1 {
		t.Errorf("hosted = %+v", hosted)
	}
	if !hosted.ExpiresAt.After(time.Now()) {
		t.Errorf("ExpiresAt = %v", hosted.ExpiresAt)
	}
}
