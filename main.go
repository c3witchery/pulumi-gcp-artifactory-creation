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

		originatorArtifactRegistryUrl := "corda-ent-docker-stable.software.r3.com"
		// used for initial testing only :: dockerImageName := "corda-ent-crypto-worker"
		tagVersion := "5.2.0.0"
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

		//Create a loop for specific image range

		//corda-os-rest-worker" "corda-os-flow-worker"
		//"corda-os-member-worker" "corda-os-p2p-gateway-worker"
		//"corda-os-p2p-link-manager-worker" "corda-os-db-worker"
		//"corda-os-flow-mapper-worker" "corda-os-verification-worker"
		//"corda-os-persistence-worker" "corda-os-token-selection-worker"
		//"corda-os-crypto-worker" "corda-os-uniqueness-worker"
		//"corda-os-plugins"

		// List of Docker images to pull, tag, and push
		images := []string{"corda-ent-rest-worker", "corda-ent-flow-worker", "corda-ent-member-worker", "corda-ent-p2p-gateway-worker",
			"corda-ent-p2p-link-manager-worker", "corda-ent-db-worker", "corda-ent-flow-mapper-worker", "corda-ent-verification-worker",
			"corda-ent-persistence-worker", "corda-ent-token-selection-worker", "corda-ent-crypto-worker", "corda-ent-uniqueness-worker",
			"corda-ent-plugins"}

		for _, imagesName := range images {
			// Function to pull Docker image
			pullImage, err := local.NewCommand(ctx, "pullImage-"+imagesName,
				&local.CommandArgs{
					Create: pulumi.String(
						" docker pull " + originatorArtifactRegistryUrl + "/" + imagesName + ":" + tagVersion,
					),
				},
				pulumi.DependsOn([]pulumi.Resource{dockerLoginToSourceArtifactory, dockerLoginToTargetArtifactory}))
			if err != nil {
				return fmt.Errorf("error pulling image command: %v", err)
			}
			ctx.Export("pullImage", pullImage.Stdout)

			//Function to tag docker image
			tagImage, err := local.NewCommand(ctx, "tagImage-"+imagesName,
				&local.CommandArgs{
					Create: pulumi.String(
						"docker tag corda/" + imagesName + ":" + tagVersion + " " + newRepositoryPath + "/" + imagesName + ":" + tagVersion,
					),
				},
				pulumi.DependsOn([]pulumi.Resource{pullImage}))
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
