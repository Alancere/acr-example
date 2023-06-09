package main

import (
	"fmt"
	"io"
	"log"

	"github.com/chrismellard/docker-credential-acr-env/pkg/credhelper"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

const (
	registriesName   = "azacrlivetest.azurecr.io"
	repositoriesName = "mygocontainerregistry"
	tag              = "latest"
)

var authOpt crane.Option
var acrPath = fmt.Sprintf("%s/%s:%s", registriesName, repositoriesName, tag)

func init() {
	// azure auth
	acrHelper := credhelper.NewACRCredentialsHelper()
	authOpt = crane.WithAuthFromKeychain(authn.NewKeychainFromHelper(acrHelper))

	// docker auth
	//dockerAuth := crane.WithAuthFromKeychain(authn.DefaultKeychain)
}

func main() {
	fmt.Println("Hello go-containerregistry")

	uploadImage()

	downloadImage()

	listRepositories()

	listTags()

	deleteImage()
}

func uploadImage() {

	// layer
	hello := []byte("hello world")
	layer := static.NewLayer(hello, types.OCIUncompressedLayer)
	diffId, err := layer.DiffID()
	if err != nil {
		log.Fatal(err)
	}

	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		log.Fatal("append layer error:", err)
	}

	// config
	img = mutate.ConfigMediaType(img, types.OCIConfigJSON)
	img, err = mutate.ConfigFile(img, &v1.ConfigFile{
		Architecture: "amd64",
		OS:           "windows",
		RootFS: v1.RootFS{
			Type:    "layers",
			DiffIDs: []v1.Hash{diffId},
		},
	})
	if err != nil {
		log.Fatal("config error:", err)
	}

	// push
	err = crane.Push(img, acrPath, authOpt)
	if err != nil {
		log.Fatal("push to remote registry error:", err)
	}
	digeset, err := img.Digest()
	fmt.Println("pushed:", digeset)
}

func downloadImage() {

	img, err := crane.Pull(fmt.Sprintf("%s/%s:%s", registriesName, repositoriesName, tag))
	if err != nil {
		log.Fatal("pull image error:", err)
	}

	// manifest
	manifest, err := img.RawManifest()
	if err != nil {
		log.Fatal("raw manifest error:", err)
	}
	fmt.Println("manifest:", string(manifest))

	// config
	conf, err := img.RawConfigFile()
	if err != nil {
		log.Fatal("config file error:", err)
	}
	fmt.Println("config:", string(conf))

	// layers
	layers, err := img.Layers()
	if err != nil {
		log.Fatal("get layers error", err)
	}
	fmt.Println("layers:")
	for _, l := range layers {
		uncompressed, err := l.Uncompressed()
		if err != nil {
			log.Fatal("uncompressed error:", err)
		}

		layer, err := io.ReadAll(uncompressed)
		if err != nil {
			log.Fatal("read layer error:", err)
		}
		fmt.Println("\t", string(layer))
	}
}

func listRepositories() {
	res, err := crane.Catalog(registriesName)
	if err != nil {
		log.Fatal("list repositories error", err)
	}
	fmt.Println("repositories:", res)
}

func listTags() {

	tags, err := crane.ListTags(fmt.Sprintf("%s/%s", registriesName, repositoriesName))
	if err != nil {
		log.Fatal("list tags error:", err)
	}
	fmt.Println("tags:", tags)
}

func deleteImage() {

	digest, err := crane.Digest(acrPath)
	if err != nil {
		log.Fatal("get digest error:", err)
	}

	// registry/repository@digest
	err = crane.Delete(fmt.Sprintf("%s/%s@%s", registriesName, repositoriesName, digest), authOpt)
	if err != nil {
		log.Fatal("delete image error:", err)
	}
	fmt.Println("deleted:", acrPath)
}
