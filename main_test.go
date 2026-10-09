package main

import (
	"context"
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/manifest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestManifestCatalogContract(t *testing.T) {
	value, err := manifest.Load(manifestJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.ValidateCatalogPresentation(value, "https://github.com/Artic0din/silo-plugin-metadata-apple"); err != nil {
		t.Fatal(err)
	}
}

func TestConfigureInvalidCountryAndUnknownSetting(t *testing.T) {
	server := &artworkServer{}
	for _, values := range []map[string]interface{}{{"english_country": "zz"}, {"unexpected": "value"}} {
		value, err := structpb.NewStruct(values)
		if err != nil {
			t.Fatal(err)
		}
		if err := server.configure(context.Background(), []*pluginv1.ConfigEntry{{Key: "artwork", Value: value}}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("accepted %v: %v", values, err)
		}
	}
	value, _ := structpb.NewStruct(map[string]interface{}{"english_country": "US"})
	if err := server.configure(context.Background(), []*pluginv1.ConfigEntry{{Key: "artwork", Value: value}}); err != nil || server.englishCountry != "us" {
		t.Fatalf("country setting: %s %v", server.englishCountry, err)
	}
}
