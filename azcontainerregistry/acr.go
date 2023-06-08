package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/streaming"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/containers/azcontainerregistry"
)

var cred azcore.TokenCredential
var acrClient *azcontainerregistry.Client

var (
	registriesName   = "https://azacrlivetest.azurecr.io"
	repositoriesName = "library/myacr2"
)

func init() {
	var err error

	cred, err = azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		log.Fatalf("failed to obtain a credential: %v", err)
	}

	acrClient, err = azcontainerregistry.NewClient(registriesName, cred, nil)
	if err != nil {
		log.Fatalf("failed to create client: %v", err)
	}
}

func main() {

	uploadImage()

	setArtifactProperties()

	downloadImage()

	listRepositories()

	listTagsWithAnonymousAccess()

	deleteImage()
}

func uploadImage() {

	ctx := context.Background()

	blobClient, err := azcontainerregistry.NewBlobClient(registriesName, cred, nil)
	if err != nil {
		log.Fatalf("failed to create blob client: %v", err)
	}

	// layer
	layer := []byte("hello world")
	startRes, err := blobClient.StartUpload(ctx, repositoriesName, nil)
	if err != nil {
		log.Fatalf("failed to start upload layer: %v", err)
	}

	calculator := azcontainerregistry.NewBlobDigestCalculator()
	uploadResp, err := blobClient.UploadChunk(ctx, *startRes.Location, bytes.NewReader(layer), calculator, nil)
	if err != nil {
		log.Fatalf("failed to upload layer: %v", err)
	}

	completeResp, err := blobClient.CompleteUpload(ctx, *uploadResp.Location, calculator, nil)
	if err != nil {
		log.Fatalf("failed to complete layer upload: %v", err)
	}

	// config
	layerDigest := *completeResp.DockerContentDigest
	config := []byte(fmt.Sprintf(`{
	architecture: "amd64",
	os: "windows",
	rootfs: {
		type: "layers",
		diff_ids: [%s],
	},
}`, layerDigest))
	startRes, err = blobClient.StartUpload(ctx, repositoriesName, nil)
	if err != nil {
		log.Fatalf("failed to start upload config: %v", err)
	}

	calculator = azcontainerregistry.NewBlobDigestCalculator()
	uploadResp, err = blobClient.UploadChunk(ctx, *startRes.Location, bytes.NewReader(config), calculator, nil)
	if err != nil {
		log.Fatalf("failed to upload config: %v", err)
	}

	completeResp, err = blobClient.CompleteUpload(ctx, *uploadResp.Location, calculator, nil)
	if err != nil {
		log.Fatalf("failed to complete config upload: %v", err)
	}

	manifest := fmt.Sprintf(`{
		"schemaVersion": 2,
		"mediaType": "application/vnd.docker.distribution.manifest.v2+json",
		"config": {
		  "mediaType": "application/vnd.oci.image.config.v1+json",
		  "digest": "%s",
		  "size": %d
		},
		"layers": [
		  {
			"mediaType": "application/vnd.oci.image.layer.v1.tar",
			"digest": "%s",
			"size": %d,
			"annotations": {
			  "title": "artifact.txt"
			}
		  }
		]
	  }`, layerDigest, len(config), *completeResp.DockerContentDigest, len(layer))

	uploadManifestRes, err := acrClient.UploadManifest(ctx, repositoriesName, "1.0.0", azcontainerregistry.ContentTypeApplicationVndDockerDistributionManifestV2JSON, streaming.NopCloser(bytes.NewReader([]byte(manifest))), nil)
	if err != nil {
		log.Fatalf("failed to upload manifest: %v", err)
	}
	fmt.Printf("digest of uploaded manifest: %s", *uploadManifestRes.DockerContentDigest)
}

func downloadImage() {

	ctx := context.Background()

	blobClient, err := azcontainerregistry.NewBlobClient(registriesName, cred, nil)
	if err != nil {
		log.Fatalf("failed to create blob client: %v", err)
	}

	// Get manifest
	manifestRest, err := acrClient.GetManifest(ctx, repositoriesName, "1.0.0", &azcontainerregistry.ClientGetManifestOptions{
		Accept: to.Ptr(string(azcontainerregistry.ContentTypeApplicationVndDockerDistributionManifestV2JSON)),
	})
	if err != nil {
		log.Fatalf("failed to get manifest: %v", err)
	}

	reader, err := azcontainerregistry.NewDigestValidationReader(*manifestRest.DockerContentDigest, manifestRest.ManifestData)
	if err != nil {
		log.Fatalf("failed to create validation reader: %v", err)
	}

	manifest, err := io.ReadAll(reader)
	if err != nil {
		log.Fatalf("failed to read manifest data: %v", err)
	}
	fmt.Printf("manifest: %s\n", manifest)

	// Get config
	var manifestJSON map[string]any
	err = json.Unmarshal(manifest, &manifestJSON)
	if err != nil {
		log.Fatalf("failed to unmarshal manifest: %v", err)
	}

	configDigest := manifestJSON["config"].(map[string]any)["digest"].(string)
	configRes, err := blobClient.GetBlob(ctx, repositoriesName, configDigest, nil)
	if err != nil {
		log.Fatalf("failed to get config: %v", err)
	}

	reader, err = azcontainerregistry.NewDigestValidationReader(configDigest, configRes.BlobData)
	if err != nil {
		log.Fatalf("failed to create validation reader: %v", err)
	}

	config, err := io.ReadAll(reader)
	if err != nil {
		log.Fatalf("failed to read config data: %v", err)
	}
	fmt.Printf("config: %s\n", config)

	// Get layers
	layers := manifestJSON["layers"].([]any)
	for _, layer := range layers {
		layerDigest := layer.(map[string]any)["digest"].(string)
		layerRes, err := blobClient.GetBlob(ctx, repositoriesName, layerDigest, nil)
		if err != nil {
			log.Fatalf("failed to get layer: %v", err)
		}

		reader, err = azcontainerregistry.NewDigestValidationReader(layerDigest, layerRes.BlobData)
		if err != nil {
			log.Fatalf("failed to create validation reader: %v", err)
		}

		f, err := os.Create(strings.Split(layerDigest, ":")[1])
		if err != nil {
			log.Fatalf("failed to create blob file: %v", err)
		}

		_, err = io.Copy(f, reader)
		if err != nil {
			log.Fatalf("failed to write to the file: %v", err)
		}

		err = f.Close()
		if err != nil {
			log.Fatalf("failed to close the file: %v", err)
		}
	}
}

func deleteImage() {
	ctx := context.Background()

	manifestPager := acrClient.NewListManifestsPager(repositoriesName, &azcontainerregistry.ClientListManifestsOptions{
		OrderBy: to.Ptr(azcontainerregistry.ArtifactManifestOrderByLastUpdatedOnDescending),
	})
	for manifestPager.More() {
		manifestPage, err := manifestPager.NextPage(ctx)
		if err != nil {
			log.Fatalf("failed to advance manifest page: %v", err)
		}
		imagesToKeep := 3
		for i, m := range manifestPage.Manifests.Attributes {
			if i >= imagesToKeep {
				for _, t := range m.Tags {
					fmt.Printf("delete tag from image: %s", *t)
					_, err := acrClient.DeleteTag(ctx, repositoriesName, *t, nil)
					if err != nil {
						log.Fatalf("failed to delete tag: %v", err)
					}
				}
				_, err := acrClient.DeleteManifest(ctx, repositoriesName, *m.Digest, nil)
				if err != nil {
					log.Fatalf("failed to delete manifest: %v", err)
				}
				fmt.Printf("delete image with digest: %s", *m.Digest)
			}
		}
	}
}

func setArtifactProperties() {
	res, err := acrClient.UpdateTagProperties(context.Background(), repositoriesName, "1.0.0", &azcontainerregistry.ClientUpdateTagPropertiesOptions{
		Value: &azcontainerregistry.TagWriteableProperties{
			CanWrite:  to.Ptr(true),
			CanDelete: to.Ptr(true),
		}})
	if err != nil {
		log.Fatalf("failed to finish the request: %v", err)
	}
	fmt.Printf("repository library/myacr - tag latest: 'CanWrite' property: %t, 'CanDelete' property: %t\n", *res.Tag.ChangeableAttributes.CanWrite, *res.Tag.ChangeableAttributes.CanDelete)
}

func listRepositories() {

	pager := acrClient.NewListRepositoriesPager(nil)
	for pager.More() {
		page, err := pager.NextPage(context.Background())
		if err != nil {
			log.Fatalf("failed to advance page: %v", err)
		}
		for _, v := range page.Repositories.Names {
			fmt.Printf("repository: %s\n", *v)
		}
	}
}

func listTagsWithAnonymousAccess() {

	pager := acrClient.NewListTagsPager("library/hello-world", nil)
	for pager.More() {
		page, err := pager.NextPage(context.Background())
		if err != nil {
			log.Fatalf("failed to advance page: %v", err)
		}
		for _, v := range page.Tags {
			fmt.Printf("tag: %s\n", *v.Name)
		}
	}
}
