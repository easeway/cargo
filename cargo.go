package main

import (
	"errors"
	"github.com/easeway/cargo/libcargo"
	"github.com/op/go-logging"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
)

var (
	optFile     = "cargo.yml"
	optDataDir  = "."
	optRegistry = ""
	optDetach   = false
	optHold     = false
	optPrepare  = true
	optCreate   = true
	optRemove   = true
	optForce    = false
	optLogVV    = false
	optLogV     = false
	optLogQ     = false
	optLogQQ    = false

	env      *cargo.CloudEnv
	clusters *cargo.Clusters

	logFormatter = logging.MustStringFormatter("%{color}%{time:15:04:05.000000} %{level:.4s} %{color:bold}[%{module}]%{color:reset} %{message}")

	errorNoDefaultCluster = errors.New("Default cluster not found")
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "cargo COMMAND",
		Short: "The Docker Orchestrator",
		Long:  "The orchestration tool to run Docker containers",
		Run:   showUsage,
	}
	rootCmd.PersistentFlags().StringVarP(&optFile, "file", "f", optFile, "Cloud Definition File")
	rootCmd.PersistentFlags().StringVarP(&optDataDir, "datadir", "d", optDataDir, "Data directory for dependent files")
	rootCmd.PersistentFlags().StringVar(&optRegistry, "registry", optRegistry, "Docker registry for caching prepared images")
	rootCmd.PersistentFlags().BoolVar(&optLogVV, "vv", optLogVV, "Extra verbose")
	rootCmd.PersistentFlags().BoolVarP(&optLogV, "verbose", "v", optLogV, "Verbose")
	rootCmd.PersistentFlags().BoolVarP(&optLogQ, "quiet", "q", optLogQ, "Quiet")
	rootCmd.PersistentFlags().BoolVar(&optLogQQ, "qq", optLogQQ, "Extra quite")

	runCmd := &cobra.Command{
		Use:   "run [CLUSTER]",
		Short: "Start, run and stop",
		Long:  "Start a cluster, run all the commands and stop",
		Run:   runCloud,
	}
	runCmd.Flags().BoolVar(&optCreate, "create", optCreate, "Re-create containers")
	runCmd.Flags().BoolVar(&optDetach, "detach", optDetach, "Detach containers instead of stop after run commands")
	runCmd.Flags().BoolVar(&optHold, "hold", optHold, "Wait Ctrl-C before stopping the containers")
	runCmd.Flags().BoolVarP(&optRemove, "remove", "r", optRemove, "Remove all containers after stop")

	rootCmd.AddCommand(runCmd)

	upCmd := &cobra.Command{
		Use:   "up [CLUSTER]",
		Short: "Start a cluster",
		Long:  "Start a cluster and run commands optionally",
		Run:   startCloud,
	}
	upCmd.Flags().BoolVar(&optCreate, "create", optCreate, "Re-create containers")
	upCmd.Flags().BoolVar(&optPrepare, "prepare", optPrepare, "Run prepare commands")
	upCmd.Flags().BoolVar(&optDetach, "detach", optDetach, "Detach containers instead of wait")
	rootCmd.AddCommand(upCmd)

	stopCmd := &cobra.Command{
		Use:   "stop [CLUSTER]",
		Short: "Stop the cluster",
		Long:  "Stop all the containers",
		Run:   stopCloud,
	}
	stopCmd.Flags().BoolVarP(&optRemove, "remove", "r", optRemove, "Remove all containers after stop")
	stopCmd.Flags().BoolVar(&optForce, "force", optForce, "Force stop/remove containers")
	rootCmd.AddCommand(stopCmd)

	rootCmd.Execute()
}

func showUsage(cmd *cobra.Command, args []string) {
	cmd.Usage()
}

func fatal(err error) {
	env.Logger.Critical("%v", err)
	os.Exit(1)
}

func ensure(err error) {
	if err != nil {
		fatal(err)
	}
}

func initEnv(args []string) {
	backend := logging.AddModuleLevel(
		logging.NewBackendFormatter(
			logging.NewLogBackend(os.Stdout, "\x1b[32mCARGO \u27a4\x1b[0m ", 0),
			logFormatter))
	if optLogVV {
		backend.SetLevel(logging.DEBUG, "")
	} else if optLogV {
		backend.SetLevel(logging.INFO, "")
	} else if optLogQQ {
		backend.SetLevel(logging.ERROR, "")
	} else if optLogQ {
		backend.SetLevel(logging.WARNING, "")
	} else {
		backend.SetLevel(logging.NOTICE, "")
	}

	env = &cargo.CloudEnv{Logger: cargo.GoLogger("env", backend)}

	var err error
	if clusters, err = cargo.LoadYaml(optFile); err != nil {
		fatal(err)
	}
	if len(args) > 0 {
		if env.Cluster = clusters.ClusterByName(args[0]); env.Cluster == nil {
			fatal(errors.New("Cluster not found: " + args[0]))
		}
	} else if env.Cluster = clusters.DefaultCluster(); env.Cluster == nil {
		fatal(errorNoDefaultCluster)
	}

	if env.DataDir, err = filepath.Abs(optDataDir); err != nil {
		fatal(err)
	}
	env.Registry = optRegistry
}

func startCloud(cmd *cobra.Command, args []string) {
	initEnv(args)
	if optCreate {
		env.RunFlags |= cargo.Create
	}
	if optPrepare {
		env.RunFlags |= cargo.Prepare
	}
	if optDetach {
		env.RunFlags |= cargo.Detach
	}
	if optRemove {
		env.RunFlags |= cargo.Remove
	}

	if state := env.Run(); state.AnyError() {
		os.Exit(1)
	}
}

func runCloud(cmd *cobra.Command, args []string) {
	initEnv(args)
	env.RunFlags |= cargo.Prepare | cargo.Run
	if optCreate {
		env.RunFlags |= cargo.Create
	}
	if optDetach {
		env.RunFlags |= cargo.Detach
	} else {
		if !optHold {
			env.RunFlags |= cargo.Stop
		}

	}
	if optRemove {
		env.RunFlags |= cargo.Remove
	}

	state := env.Run()
	if state.AnyError() {
		state.StopAndWait()
		os.Exit(1)
	}

	if !optDetach {
		if optHold {
			// TODO wait for stop
		}
		state.StopAndWait()
	}
}

func stopCloud(cmd *cobra.Command, args []string) {
	initEnv(args)
	// TODO
}
