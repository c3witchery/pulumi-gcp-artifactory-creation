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
		gcpProject := ""

		originatorArtifactRegistryUrl := "corda-ent-docker-stable.software.r3.com"
		
		tagVersion := "5.2.1.0-RC02"

		newRepositoryName := "private-docker-repo"
		newRepositoryPath := region + "-docker.pkg.dev/" + gcpProject + "/" + newRepositoryName
		newRepositoryURL := "https://" + newRepositoryPath

		// Source Artifactory credentials
		sourceArtifactoryUserName := "$CORDA_ARTIFACTORY_USERNAME"
		sourceArtifactoryPassword := "$CORDA_ARTIFACTORY_PASSWORD"

		//local load for non-released images
		localSystemPath := "/tmp/docker-images/"

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

		//Create local directory
		destDir := localSystemPath + newRepositoryName + "/"
		createLocalDirectory, err := local.NewCommand(ctx, "createLocalDirectory", &local.CommandArgs{
			Create: pulumi.String(" mkdir -p " + destDir + " \n " + "chmod +x " + destDir + " \n "),
		}, pulumi.DependsOn([]pulumi.Resource{newRepository}))
		if err != nil {
			return fmt.Errorf("error with creation of local directory for the docker images from Staging: %v", err)
		}
		ctx.Export("createLocalDirectory", createLocalDirectory.Stdout)

		//Download and extract tgz file
		downloadTar, err := local.NewCommand(ctx, "download-tar", &local.CommandArgs{
			Create: pulumi.String("wget " + stagingPath + " -r -P " + destDir),
		},
			pulumi.DependsOn([]pulumi.Resource{createLocalDirectory}))
		if err != nil {
			return fmt.Errorf("error with downloading the docker images via Staging: %v", err)
		}
		ctx.Export("downloadTar", downloadTar.Stdout)

		//// Load the local image
		//// this has to be tailored to a tgz local file downloaded from Staging
		loadImage, err := local.NewCommand(ctx, "loadImage", &local.CommandArgs{
			Create: pulumi.String("docker load -i " + destDir + "staging.download.corda.net/c5-release-pack/20ede3c6-29c0-11ed-966d-b7c36748b9f6-RC02/corda-ent-worker-images-RC02.tar.gz"),
		},
			pulumi.DependsOn([]pulumi.Resource{downloadTar}))
		if err != nil {
			return fmt.Errorf("error loading image command: %v", err)
		}
		ctx.Export("loadImage", loadImage.Stdout)

		// List of Docker images to pull, tag, and push
		images := []string{"corda-ent-rest-worker", "corda-ent-flow-worker", "corda-ent-member-worker", "corda-ent-p2p-gateway-worker",
			"corda-ent-p2p-link-manager-worker", "corda-ent-db-worker", "corda-ent-flow-mapper-worker", "corda-ent-verification-worker",
			"corda-ent-persistence-worker", "corda-ent-token-selection-worker", "corda-ent-crypto-worker", "corda-ent-uniqueness-worker",
			"corda-ent-plugins"}

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

			//// build the local image if isReleased set to false
			//// this has to be tailored to OCI Registry syntax
			//// oras is assumed to be installed here
			// loadImage, err := local.NewCommand(ctx, "loadImage"+imagesName, &local.CommandArgs{
			// 	Create: pulumi.String("oras pull --oci-layout " + destDir + "corda/" + imagesName + ":" + tagVersion),
			// 	Triggers: pulumi.Array{
			// 		pulumi.Bool(!isReleased),
			// 	},
			// },
			// 	pulumi.DependsOn([]pulumi.Resource{pullImage}))
			// if err != nil {
			// 	return fmt.Errorf("error loading image command: %v", err)
			// }
			// ctx.Export("loadImage", loadImage.Stdout)

			//Function to tag docker image
			tagImage, err := local.NewCommand(ctx, "tagImage-"+imagesName,
				&local.CommandArgs{
					Create: pulumi.String(
						"docker tag corda/" + imagesName + ":" + tagVersion + " " + newRepositoryPath + "/" + imagesName + ":" + tagVersion,
					),
				}, pulumi.DependsOn([]pulumi.Resource{pullImage, loadImage}))
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
