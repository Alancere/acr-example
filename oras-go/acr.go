package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/opencontainers/image-spec/specs-go"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

const (
	registriesName   = "azacrlivetest.azurecr.io"
	repositoriesName = "myoras"
	tag              = "latest"
	userName         = "azacrlivetest"
	userPassword     = ""
)

var remoteRegistry *remote.Registry
var remoteRepository registry.Repository

func init() {
	var err error

	// Connect to a remote repository
	remoteRegistry, err = remote.NewRegistry(registriesName)
	if err != nil {
		log.Fatal("new registry err:", err)
	}

	remoteRegistry.Client = &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.DefaultCache,
		Credential: auth.StaticCredential(registriesName, auth.Credential{
			// or AccessToken
			Username: userName,
			Password: userPassword,
		}),
	}

	remoteRepository, err = remoteRegistry.Repository(context.Background(), repositoriesName)
	if err != nil {
		log.Fatal("new repo err:", err)
	}
}

func main() {
	fmt.Println("hello oras")

	uploadImage()

	pullImage()

	listReporitories()

	listTags()

	deleteImage()
}

func pullImage() {
	store := memory.New()
	ctx := context.Background()

	manifestDescriptor, err := oras.Copy(ctx, remoteRepository, tag, store, tag, oras.DefaultCopyOptions)
	if err != nil {
		log.Fatal("copy err:", err)
	}
	fmt.Println("manifest pulled:", manifestDescriptor.Digest, manifestDescriptor.MediaType)

	f, err := store.Fetch(ctx, manifestDescriptor)
	if err != nil {
		log.Fatal("fetch err:", err)
	}

	data := make([]byte, manifestDescriptor.Size)
	_, err = f.Read(data)
	if err != nil {
		log.Fatal("read err:", err)
	}

	fmt.Printf("manifest content:\n%s", data)
}

func uploadImage() {
	ctx := context.Background()
	store := memory.New()

	// layer
	layer := []byte("hello oras")
	layerDescriptor := content.NewDescriptorFromBytes(v1.MediaTypeImageLayer, layer)

	err := store.Push(ctx, layerDescriptor, bytes.NewReader(layer))
	if err != nil {
		log.Fatal("layer push err:", err)
	}

	// config
	config := []byte(fmt.Sprintf(`{
        architecture: "amd64",
        os: "windows",
        rootfs: {
            type: "layers",
            diff_ids: [%s],
        },
    }`, layerDescriptor.Digest))
	configDescriptor := content.NewDescriptorFromBytes(v1.MediaTypeImageConfig, config)

	err = store.Push(ctx, configDescriptor, bytes.NewReader(config))
	if err != nil {
		log.Fatal("config push err:", err)
	}

	// manifest
	ManifestContent := v1.Manifest{
		Layers:    []v1.Descriptor{layerDescriptor},
		Config:    configDescriptor,
		Versioned: specs.Versioned{SchemaVersion: 2},
	}

	manifest, err := json.Marshal(ManifestContent)
	if err != nil {
		log.Fatal("manifest marshal err:", err)
	}

	manifestDescriptor := content.NewDescriptorFromBytes(v1.MediaTypeImageManifest, manifest)

	err = store.Push(ctx, manifestDescriptor, bytes.NewReader(manifest))
	if err != nil {
		log.Fatal("manifest push err:", err)
	}

	err = store.Tag(ctx, manifestDescriptor, tag)
	if err != nil {
		log.Fatal("tag err(tag not exist):", err)
	}

	// local to remote
	descriptor, err := oras.Copy(ctx, store, tag, remoteRepository, tag, oras.DefaultCopyOptions)
	if err != nil {
		log.Fatal("upload image err:", err)
	}
	fmt.Println("digest of uploaded manifest:", descriptor.Digest)
}

func deleteImage() {

	manifest := remoteRepository.Manifests()
	ref := fmt.Sprintf("%s/%s:%s", registriesName, repositoriesName, tag)
	manifestDescriptor, err := manifest.Resolve(context.Background(), ref)
	if err != nil {
		log.Fatal("resolve reference:", err)
	}

	err = manifest.Delete(context.Background(), manifestDescriptor)
	if err != nil {
		log.Fatal("delete repository err manifest:", err)
	}
}

func listReporitories() {

	repos, err := registry.Repositories(context.Background(), remoteRegistry)
	if err != nil {
		log.Fatal("list repositories err:", err)
	}
	fmt.Println("Repositories:", repos)
}

func listTags() {

	tags, err := registry.Tags(context.Background(), remoteRepository)
	if err != nil {
		log.Fatal("list tags err:", err)
	}
	fmt.Printf("%s repository tags: %s\n", repositoriesName, tags)
}
