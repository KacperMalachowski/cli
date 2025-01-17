package app

import (
	"fmt"
	"github.com/kyma-project/cli.v3/internal/kube"
	"os"
	"time"

	"github.com/kyma-project/cli.v3/internal/clierror"
	"github.com/kyma-project/cli.v3/internal/cmdcommon"
	"github.com/kyma-project/cli.v3/internal/cmdcommon/types"
	"github.com/kyma-project/cli.v3/internal/dockerfile"
	"github.com/kyma-project/cli.v3/internal/kube/resources"
	"github.com/kyma-project/cli.v3/internal/registry"
	"github.com/spf13/cobra"
)

type appPushConfig struct {
	*cmdcommon.KymaConfig

	name                 string
	namespace            string
	image                string
	dockerfilePath       string
	dockerfileSrcContext string
	containerPort        types.NullableInt64
	istioInject          types.NullableBool
	expose               bool
}

func NewAppPushCMD(kymaConfig *cmdcommon.KymaConfig) *cobra.Command {
	config := appPushConfig{
		KymaConfig: kymaConfig,
	}

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push the application to the Kubernetes cluster.",
		Long:  "Use this command to push the application to the Kubernetes cluster.",

		PreRun: func(_ *cobra.Command, args []string) {
			clierror.Check(config.complete())
			clierror.Check(config.validate())
		},
		Run: func(_ *cobra.Command, _ []string) {
			clierror.Check(runAppPush(&config))
		},
	}

	cmd.Flags().StringVar(&config.name, "name", "", "Name of the app")
	cmd.Flags().StringVar(&config.namespace, "namespace", "default", "Namespace where app should be deployed")
	cmd.Flags().StringVar(&config.image, "image", "", "Name of the image to deploy")
	cmd.Flags().StringVar(&config.dockerfilePath, "dockerfile", "", "Path to the dockerfile")
	cmd.Flags().StringVar(&config.dockerfileSrcContext, "dockerfile-context", "", "Context path for building dockerfile")
	cmd.Flags().Var(&config.containerPort, "container-port", "Port on which the application will be exposed")
	cmd.Flags().Var(&config.istioInject, "istio-inject", "Enable Istio for the app")
	cmd.Flags().BoolVar(&config.expose, "expose", false, "Creates an ApiRule for the app")

	_ = cmd.MarkFlagRequired("name")
	cmd.MarkFlagsMutuallyExclusive("image", "dockerfile")
	cmd.MarkFlagsMutuallyExclusive("image", "dockerfile-context")
	cmd.MarkFlagsOneRequired("image", "dockerfile")

	return cmd
}

func (apc *appPushConfig) complete() clierror.Error {
	var err error
	var info os.FileInfo

	if apc.dockerfilePath != "" {
		// add /Dockerfile suffix if path is a directory
		info, err = os.Stat(apc.dockerfilePath)
		if err != nil {
			return clierror.Wrap(err, clierror.New(fmt.Sprintf("failed to get stat info for path: %s", apc.dockerfilePath)))
		}
		if info.IsDir() {
			apc.dockerfilePath = fmt.Sprintf("%s/Dockerfile", apc.dockerfilePath)
		}

		// set dockerfile context to working directory if its empty
		if apc.dockerfileSrcContext == "" {
			apc.dockerfileSrcContext, err = os.Getwd()
			if err != nil {
				return clierror.Wrap(err, clierror.New("failed to get current working directory",
					"Please provide the path to the dockerfile context using --dockerfile-context flag"))
			}
		}
	}

	return nil
}

func (apc *appPushConfig) validate() clierror.Error {
	if apc.expose && apc.containerPort.Value == nil {
		return clierror.New("container-port is required when expose is enabled")
	}
	return nil
}

func runAppPush(cfg *appPushConfig) clierror.Error {
	image := cfg.image
	imagePullSecret := ""

	client, clierr := cfg.GetKubeClientWithClierr()
	if clierr != nil {
		return clierr
	}

	if cfg.dockerfilePath != "" {
		registryConfig, cliErr := registry.GetInternalConfig(cfg.Ctx, client)
		if cliErr != nil {
			return clierror.WrapE(cliErr, clierror.New("failed to load in-cluster registry configuration"))
		}

		image, clierr = buildAndImportImage(client, cfg, registryConfig)
		if clierr != nil {
			return clierr
		}
		imagePullSecret = registryConfig.SecretName
	}

	fmt.Printf("\nCreating deployment %s/%s\n", cfg.namespace, cfg.name)

	err := resources.CreateDeployment(cfg.Ctx, client, cfg.name, cfg.namespace, image, imagePullSecret, cfg.istioInject)
	if err != nil {
		return clierror.Wrap(err, clierror.New("failed to create deployment"))
	}

	if cfg.containerPort.Value != nil {
		fmt.Printf("\nCreating service %s/%s\n", cfg.namespace, cfg.name)
		err = resources.CreateService(cfg.Ctx, client, cfg.name, cfg.namespace, int32(*cfg.containerPort.Value))
		if err != nil {
			return clierror.Wrap(err, clierror.New("failed to create service"))
		}
	}

	if cfg.expose {
		fmt.Printf("\nCreating API Rule %s/%s\n", cfg.namespace, cfg.name)
		var domain string
		domain, clierr = client.Istio().GetClusterAddressFromGateway(cfg.Ctx)
		if clierr != nil {
			return clierror.WrapE(clierr, clierror.New("failed to get cluster address from gateway", "Make sure Istio module is installed"))
		}

		err = resources.CreateAPIRule(cfg.Ctx, client.RootlessDynamic(), cfg.name, cfg.namespace, domain, uint32(*cfg.containerPort.Value))
		if err != nil {
			return clierror.Wrap(err, clierror.New("failed to create API Rule", "Make sure API Gateway module is installed", "Make sure APIRule is available in v2alpha1 version"))
		}
	}

	return nil
}

func buildAndImportImage(client kube.Client, cfg *appPushConfig, registryConfig *registry.InternalRegistryConfig) (string, clierror.Error) {
	fmt.Println("Building image")
	imageName, err := buildImage(cfg)
	if err != nil {
		return "", clierror.Wrap(err, clierror.New("failed to build image from dockerfile"))
	}

	fmt.Println("\nImporting", imageName)
	pushedImage, cliErr := registry.ImportImage(
		cfg.Ctx,
		imageName,
		registry.ImportOptions{
			ClusterAPIRestConfig: client.RestConfig(),
			RegistryAuth:         registry.NewBasicAuth(registryConfig.SecretData.Username, registryConfig.SecretData.Password),
			RegistryPullHost:     registryConfig.SecretData.PullRegAddr,
			RegistryPodName:      registryConfig.PodMeta.Name,
			RegistryPodNamespace: registryConfig.PodMeta.Namespace,
			RegistryPodPort:      registryConfig.PodMeta.Port,
		},
	)
	if cliErr != nil {
		return "", clierror.WrapE(cliErr, clierror.New("failed to import image to in-cluster registry"))
	}

	return pushedImage, nil
}

func buildImage(cfg *appPushConfig) (string, error) {
	imageTag := time.Now().Format("2006-01-02_15-04-05")
	imageName := fmt.Sprintf("%s:%s", cfg.name, imageTag)

	err := dockerfile.Build(cfg.Ctx, &dockerfile.BuildOptions{
		ImageName:      imageName,
		BuildContext:   cfg.dockerfileSrcContext,
		DockerfilePath: cfg.dockerfilePath,
	})
	if err != nil {
		return "", err
	}

	return imageName, nil
}
