package main

import (
	"fmt"
	"unicode"

	"archive/tar"
	"compress/gzip"
	"io"
	"net/http"
	"os"

	"github.com/pulumi/pulumi-command/sdk/go/command/local"
	"github.com/pulumi/pulumi-gcp/sdk/v7/go/gcp/artifactregistry"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {

		region := "us-central1"
		gcpProject := "r3-ps-test-01"

		originatorArtifactRegistryUrl := "corda-ent-docker-stable.software.r3.com"
		stagingPath := "https://staging.download.corda.net/c5-release-pack/20ede3c6-29c0-11ed-966d-b7c36748b9f6-RC02/corda-ent-worker-images-RC02.tar.gz"

		tagVersion := "5.2.1.0-RC02"
		isReleased := hasLitteral(tagVersion)

		newRepositoryName := "private-docker-repo"
		newRepositoryPath := region + "-docker.pkg.dev/" + gcpProject + "/" + newRepositoryName
		newRepositoryURL := "https://" + newRepositoryPath

		// Source Artifactory credentials
		sourceArtifactoryUserName := "$CORDA_ARTIFACTORY_USERNAME"
		sourceArtifactoryPassword := "$CORDA_ARTIFACTORY_PASSWORD"

		//New Repository created
		newRepository, err := artifactregistry.NewRepository(ctx, newRepositoryName, &artifactregistry.RepositoryArgs{
			Location:     pulumi.String(region),
			RepositoryId: pulumi.String(newRepositoryName),
			Description:  pulumi.String("Private Docker repository"),
			Format:       pulumi.String("DOCKER"),
		})
		if err != nil {
			return fmt.Errorf("error creating destination repository: %v", err)
		}
		ctx.Export("newRepository", newRepository.RepositoryId)

		//create a secure connection via gcloud cli
		connectToGcpRepository, err := local.NewCommand(ctx, "connectToGcpRepository", &local.CommandArgs{
			Create: pulumi.String("gcloud auth configure-docker " + region + "-docker.pkg.dev"),
		},
			pulumi.DependsOn([]pulumi.Resource{newRepository}))
		if err != nil {
			return fmt.Errorf("error login to the new repository via gcloud: %v", err)
		}
		ctx.Export("connectToGcpRepository", connectToGcpRepository.Stdout)

		//check if the release is released to the semi-public artefact repository or has to be downloaded from Staging site

		//docker login
		dockerLoginToSourceArtifactory, err := local.NewCommand(ctx, "dockerLoginToSourceArtifactory",
			&local.CommandArgs{
				Create: pulumi.String(
					" docker login " + originatorArtifactRegistryUrl + " -u " + sourceArtifactoryUserName + " -p " + sourceArtifactoryPassword,
				),
			})
		if err != nil {
			return fmt.Errorf("error with docker login from the source artifactory: %v", err)
		}
		ctx.Export("dockerLoginToSourceArtifactory", dockerLoginToSourceArtifactory.Stdout)

		//download and extract tgz file
		//destination directory
		destDir := "/tmp/docker-images/" + newRepositoryName + "/"
		downloadAndExtract(isReleased, stagingPath, destDir)
		ctx.Export("downloadAndExtract", pulumi.String("Downloaded and Extracted to: "+destDir))

		//docker login to Targegt Artifactory
		dockerLoginToTargetArtifactory, err := local.NewCommand(ctx, "dockerLoginToTargetArtifactory",
			&local.CommandArgs{
				Create: pulumi.String(
					"docker login " + newRepositoryURL,
				),
			},
			pulumi.DependsOn([]pulumi.Resource{connectToGcpRepository}))
		if err != nil {
			return fmt.Errorf("error with docker login from the target artifactory: %v", err)
		}
		ctx.Export("dockerLoginToTargetArtifactory", dockerLoginToTargetArtifactory.Stdout)

		// List of Docker images to pull, tag, and push
		images := []string{"corda-ent-rest-worker", "corda-ent-flow-worker", "corda-ent-member-worker", "corda-ent-p2p-gateway-worker",
			"corda-ent-p2p-link-manager-worker", "corda-ent-db-worker", "corda-ent-flow-mapper-worker", "corda-ent-verification-worker",
			"corda-ent-persistence-worker", "corda-ent-token-selection-worker", "corda-ent-crypto-worker", "corda-ent-uniqueness-worker",
			"corda-ent-plugins"}

		//Create a loop for specific image range
		for _, imagesName := range images {

			// Function to pull Docker image if it is released from source Artifactory
			//otherwise it look for the downloaded and extracted version from Staging
			pullOrLoadImage, err := local.NewCommand(ctx, "pull-or-load-image-"+imagesName,
				&local.CommandArgs{
					Create: pulumi.String(
						" docker " + pullOrLoad(isReleased) + " " + dynamicOriginalRepositoryPath(isReleased) + imagesName + ":" + tagVersion,
					),
				},
				pulumi.DependsOn([]pulumi.Resource{dockerLoginToTargetArtifactory}))
			if err != nil {
				return fmt.Errorf("error pulling image command: %v", err)
			}
			ctx.Export("pullOrLoadImage", pullOrLoadImage.Stdout)

			//Function to tag docker image
			tagImage, err := local.NewCommand(ctx, "tagImage-"+imagesName,
				&local.CommandArgs{
					Create: pulumi.String(
						"docker tag corda/" + imagesName + ":" + tagVersion + " " + newRepositoryPath + "/" + imagesName + ":" + tagVersion,
					),
				}, pulumi.DependsOn([]pulumi.Resource{pullOrLoadImage}))
			if err != nil {
				return fmt.Errorf("error tagging image command: %v", err)
			}
			ctx.Export("tagImage", tagImage.Stdout)

			// Function to push Docker image
			pushImage, err := local.NewCommand(ctx, "pushImage-"+imagesName,
				&local.CommandArgs{
					Create: pulumi.String(
						"docker push " + newRepositoryPath + "/" + imagesName + ":" + tagVersion,
					),
				},
				pulumi.DependsOn([]pulumi.Resource{tagImage}))
			if err != nil {
				return fmt.Errorf("error pushing image command: %v", err)
			}
			ctx.Export("pushImages", pushImage.Stdout)
		}

		return nil
	})
}

func pullOrLoad(isReleased bool) string {
	if isReleased {
		return " pull "
	} else {
		return " load -i "
	}
}

func hasLitteral(tagVersion string) bool {
	for _, r := range tagVersion {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func dynamicOriginalRepositoryPath(isReleased bool) string {
	if isReleased {
		originatorArtifactRegistryUrl := "corda-ent-docker-stable.software.r3.com/"
		return originatorArtifactRegistryUrl
	} else {
		return ""
	}

}

func downloadAndExtract(isReleased bool, stagingPath string, destDir string) error {
	if !isReleased {
		resp, err := http.Get(stagingPath)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return err
		}
		defer gzReader.Close()

		tarReader := tar.NewReader(gzReader)
		for {
			header, err := tarReader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}

			target := destDir + "/" + header.Name
			switch header.Typeflag {
			case tar.TypeDir:
				if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
					return err
				}
			case tar.TypeReg:
				file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, os.FileMode(header.Mode))
				if err != nil {
					return err
				}
				if _, err := io.Copy(file, tarReader); err != nil {
					return err
				}
				file.Close()
			default:
				return fmt.Errorf("unsupported header type: %v", header)
			}
		}
		return nil

	}
	return nil
}
