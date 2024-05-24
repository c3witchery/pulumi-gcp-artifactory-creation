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
		dockerImageName := "corda-ent-crypto-worker"
		tagVersion := "5.2.0.0"
		newRepositoryName := "private-docker-repository"
		newRepositoryPath := region + "-docker.pkg.dev/" + gcpProject + "/" + newRepositoryName
		newRepositoryURL := "https://" + newRepositoryPath

		// Source Artifactory credentials
		sourceArtifactoryUserName := "$CORDA_ARTIFACTORY_USERNAME"
		sourceArtifactoryPassword := "$CORDA_ARTIFACTORY_PASSWORD"

		//New Repository created
		newRepository, err := artifactregistry.NewRepository(ctx, newRepositoryName, &artifactregistry.RepositoryArgs{
			Location:     pulumi.String(region),
			RepositoryId: pulumi.String("private-docker-repo"),
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

		// Function to pull Docker image
		pullImages, err := local.NewCommand(ctx, "pullImages",
			&local.CommandArgs{
				Create: pulumi.String(
					" docker pull " + originatorArtifactRegistryUrl + "/" + dockerImageName + ":" + tagVersion,
				),
			},
			pulumi.DependsOn([]pulumi.Resource{dockerLoginToSourceArtifactory, dockerLoginToTargetArtifactory}))
		if err != nil {
			return fmt.Errorf("error pulling image command: %v", err)
		}
		ctx.Export("pullImages", pullImages.Stdout)

		//Function to tag docker image
		tagImages, err := local.NewCommand(ctx, "tagImages",
			&local.CommandArgs{
				Create: pulumi.String(
					"docker tag corda/" + dockerImageName + ":" + tagVersion + " " + newRepositoryPath + "/" + dockerImageName + ":" + tagVersion,
				),
			},
			pulumi.DependsOn([]pulumi.Resource{pullImages}))
		if err != nil {
			return fmt.Errorf("error tagging image command: %v", err)
		}
		ctx.Export("tagImages", tagImages.Stdout)

		// Function to push Docker image
		pushImages, err := local.NewCommand(ctx, "pushImages",
			&local.CommandArgs{
				Create: pulumi.String(
					"docker push " + newRepositoryPath + "/" + dockerImageName + ":" + tagVersion,
				),
			},
			pulumi.DependsOn([]pulumi.Resource{tagImages}))
		if err != nil {
			return fmt.Errorf("error pushing image command: %v", err)
		}
		ctx.Export("pushImages", pushImages.Stdout)

		return nil
	})
}
