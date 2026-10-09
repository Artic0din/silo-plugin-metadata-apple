package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtime"
	"github.com/hashicorp/go-plugin"
)

func TestBinarySDKHandshakeAndRPCs(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "apple-artwork")
	command := exec.Command("go", "build", "-o", binary, ".")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build: %s %v", output, err)
	}
	process := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: runtime.HandshakeConfig(),
		Plugins:         runtime.DefaultPluginSet(runtime.CapabilityServers{}),
		Cmd:             exec.Command(binary), AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
	})
	defer process.Kill()
	rpc, err := process.Client()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := rpc.Dispense(runtime.PluginSetName)
	if err != nil {
		t.Fatal(err)
	}
	client, ok := raw.(*runtime.Client)
	if !ok {
		t.Fatalf("unexpected SDK client %T", raw)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := client.Runtime().GetManifest(ctx, &pluginv1.GetManifestRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest.PluginId != "artic0din.apple-artwork" || result.Manifest.Checksum == "" {
		t.Fatal("runtime manifest identity or checksum missing")
	}
	if _, err := client.Runtime().Configure(ctx, &pluginv1.ConfigureRequest{}); err != nil {
		t.Fatal(err)
	}
	images, err := client.MetadataProvider().GetImages(ctx, &pluginv1.GetImagesRequest{ItemType: "movie"})
	if err != nil || len(images.Images) != 0 {
		t.Fatalf("empty request RPC: %v %v", images, err)
	}
	path, err := imagePath(testImage("test", "nr", 2000, 3000).URL, 2000, 3000, "jpg")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := client.ImageResolver().ResolveImageURL(ctx, &pluginv1.ResolveImageURLRequest{Path: path, Variant: "card"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Url != "https://is1-ssl.mzstatic.com/image/thumb/test/400x600nr.jpg" {
		t.Fatalf("unexpected URL: %s", resolved.Url)
	}
}
