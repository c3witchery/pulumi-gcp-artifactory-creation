package main

import (
	"fmt"

	"github.com/pulumi/pulumi-command/sdk/go/command/local"
	"github.com/pulumi/pulumi-gcp/sdk/v7/go/gcp/artifactregistry"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {

		region := "us-central1"
		gcpProject := "r3-ps-test-01"

		originatorArtifactRegistryUrl := "docker.io"
		//originatorArtifactRegistryUrl := "corda-ent-docker-stable.software.r3.com"
		// Staging path for Corda ENT 5.2.1-RC2
		//stagingPath := "https://staging.download.corda.net/c5-release-pack/20ede3c6-29c0-11ed-966d-b7c36748b9f6-RC02/corda-ent-worker-images-RC02.tar.gz"
		// Staging path for Corda ENT 5.2.1-GA
		//stagingPath := "https://staging.download.corda.net/c5-release-pack/20ede3c6-29c0-11ed-966d-b7c36748b9f6-5.2.1-GA/corda-ent-worker-images-5.2.1-GA.tar.gz"
		// docker pull corda/corda-enterprise:4.12-zulu-openjdk-alpine

		tagVersion := "4.12-zulu-openjdk-alpine"

		newRepositoryName := "private-docker-repo"
		newRepositoryPath := region + "-docker.pkg.dev/" + gcpProject + "/" + newRepositoryName
		newRepositoryURL := "https://" + newRepositoryPath

		// Source Artifactory credentials
		// sourceArtifactoryUserName := "$CORDA_ARTIFACTORY_USERNAME"
		// sourceArtifactoryPassword := "$CORDA_ARTIFACTORY_PASSWORD"

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

		// //docker login
		// dockerLoginToSourceArtifactory, err := local.NewCommand(ctx, "dockerLoginToSourceArtifactory",
		// 	&local.CommandArgs{
		// 		Create: pulumi.String(
		// 			" docker login " + originatorArtifactRegistryUrl + " -u " + sourceArtifactoryUserName + " -p " + sourceArtifactoryPassword,
		// 		),
		// 	})
		// if err != nil {
		// 	return fmt.Errorf("error with docker login from the source artifactory: %v", err)
		// }
		// ctx.Export("dockerLoginToSourceArtifactory", dockerLoginToSourceArtifactory.Stdout)

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

		// //// Load the local image
		// //// this has to be tailored to a tgz local file downloaded from Staging
		// loadImage, err := local.NewCommand(ctx, "loadImage", &local.CommandArgs{
		// 	Create: pulumi.String("docker load -i " + "docker.io/corda/corda-enterprise:" + tagVersion),
		// },
		// 	pulumi.DependsOn([]pulumi.Resource{dockerLoginToTargetArtifactory}))
		// //pulumi.DependsOn([]pulumi.Resource{downloadTar}))
		// if err != nil {
		// 	return fmt.Errorf("error loading image command: %v", err)
		// }
		// ctx.Export("loadImage", loadImage.Stdout)

		// List of Docker images to pull, tag, and push
		images := []string{"corda/corda-enterprise"}

		//Create a loop for specific image range
		for _, imagesName := range images {

			// Function to pull Docker image if it is released from source Artifactory
			//otherwise it look for the downloaded and extracted version from Staging
			pullImage, err := local.NewCommand(ctx, "pull-image-"+imagesName,
				&local.CommandArgs{
					Create: pulumi.String(
						" docker pull " + originatorArtifactRegistryUrl + "/" + imagesName + ":" + tagVersion,
					),
				},
				pulumi.DependsOn([]pulumi.Resource{dockerLoginToTargetArtifactory}))
			if err != nil {
				return fmt.Errorf("error pulling image command: %v", err)
			}
			ctx.Export("pullImage", pullImage.Stdout)

			//Function to tag docker image
			tagImage, err := local.NewCommand(ctx, "tagImage-"+imagesName,
				&local.CommandArgs{
					Create: pulumi.String(
						"docker tag " + imagesName + ":" + tagVersion + " " + newRepositoryPath + "/" + imagesName + ":" + tagVersion,
					),
				}, pulumi.DependsOn([]pulumi.Resource{pullImage, dockerLoginToTargetArtifactory}))
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
