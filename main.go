/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"github.com/spf13/pflag"
	"k8s.io/klog"
	"net/http"
	_ "net/http/pprof"
	"os"
	infrav1exp "sigs.k8s.io/cluster-api-provider-gcp/exp/api/v1alpha3"
	infrav1controllersexp "sigs.k8s.io/cluster-api-provider-gcp/exp/controllers"
	"sigs.k8s.io/cluster-api-provider-gcp/feature"
	clusterv1exp "sigs.k8s.io/cluster-api/exp/api/v1alpha3"
	capifeature "sigs.k8s.io/cluster-api/feature"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/klogr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	infrav1 "sigs.k8s.io/cluster-api-provider-gcp/api/v1alpha3"
	"sigs.k8s.io/cluster-api-provider-gcp/controllers"
	// +kubebuilder:scaffold:imports
)

var (
	scheme                            = runtime.NewScheme()
	setupLog                          = ctrl.Log.WithName("setup")
	metricsAddr                       string
	enableLeaderElection              bool
	leaderElectionNamespace           string
	watchNamespace                    string
	profilerAddress                   string
	gcpClusterConcurrency             int
	gcpMachineConcurrency             int
	gcpManagedClusterConcurrency      int
	gcpManagedControlPlaneConcurrency int
	gcpManagedMachinePoolConcurrency  int
	syncPeriod                        time.Duration
	webhookPort                       int
	healthAddr                        string
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = infrav1.AddToScheme(scheme)
	_ = infrav1exp.AddToScheme(scheme)
	_ = clusterv1.AddToScheme(scheme)
	_ = clusterv1exp.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func InitFlags(fs *pflag.FlagSet) {
	klog.InitFlags(nil)

	fs.StringVar(
		&metricsAddr,
		"metrics-addr",
		":8080",
		"The address the metric endpoint binds to.",
	)

	fs.BoolVar(
		&enableLeaderElection,
		"enable-leader-election",
		false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.",
	)

	fs.StringVar(
		&watchNamespace,
		"namespace",
		"",
		"Namespace that the controller watches to reconcile cluster-api objects. If unspecified, the controller watches for cluster-api objects across all namespaces.",
	)

	fs.StringVar(
		&leaderElectionNamespace,
		"leader-election-namespace",
		"",
		"Namespace that the controller performs leader election in. If unspecified, the controller will discover which namespace it is running in.",
	)

	fs.StringVar(
		&profilerAddress,
		"profiler-address",
		"",
		"Bind address to expose the pprof profiler (e.g. localhost:6060)",
	)

	fs.IntVar(&gcpClusterConcurrency,
		"gcpcluster-concurrency",
		10,
		"Number of GCPClusters to process simultaneously",
	)

	fs.IntVar(&gcpMachineConcurrency,
		"gcpmachine-concurrency",
		10,
		"Number of GCPMachines to process simultaneously",
	)

	fs.IntVar(&gcpManagedClusterConcurrency,
		"gcpmanagedcluster-concurrency",
		10,
		"Number of GCPManagedClusters to process simultaneously",
	)

	fs.IntVar(&gcpManagedControlPlaneConcurrency,
		"gcpmanagedcontrolplane-concurrency",
		10,
		"Number of GCPManagedControlPlane to process simultaneously",
	)

	fs.IntVar(&gcpManagedMachinePoolConcurrency,
		"gcpmanagedmachinepool-concurrency",
		10,
		"Number of GCPManagedMachinePool to process simultaneously",
	)

	fs.DurationVar(&syncPeriod,
		"sync-period",
		10*time.Minute,
		"The minimum interval at which watched resources are reconciled (e.g. 15m)",
	)

	fs.IntVar(&webhookPort,
		"webhook-port",
		0,
		"Webhook Server port, disabled by default. When enabled, the manager will only work as webhook server, no reconcilers are installed.",
	)

	fs.StringVar(&healthAddr,
		"health-addr",
		":9440",
		"The address the health endpoint binds to.",
	)

	feature.MutableGates.AddFlag(fs)
}

func main() {
	InitFlags(pflag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	if watchNamespace != "" {
		setupLog.Info("Watching cluster-api objects only in namespace for reconciliation", "namespace", watchNamespace)
	}

	if profilerAddress != "" {
		setupLog.Info("Profiler listening for requests", "profiler-address", profilerAddress)
		go func() {
			setupLog.Error(http.ListenAndServe(profilerAddress, nil), "listen and serve error")
		}()
	}

	ctrl.SetLogger(klogr.New())

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                  scheme,
		MetricsBindAddress:      metricsAddr,
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "controller-leader-election-capg",
		LeaderElectionNamespace: leaderElectionNamespace,
		SyncPeriod:              &syncPeriod,
		Namespace:               watchNamespace,
		Port:                    webhookPort,
		HealthProbeBindAddress:  healthAddr,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Initialize event recorder.
	record.InitFromRecorder(mgr.GetEventRecorderFor("gcp-controller"))

	if webhookPort == 0 {
		if err = (&controllers.GCPMachineReconciler{
			Client: mgr.GetClient(),
			Log:    ctrl.Log.WithName("controllers").WithName("GCPMachine"),
		}).SetupWithManager(mgr, controller.Options{MaxConcurrentReconciles: gcpMachineConcurrency}); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "GCPMachine")
			os.Exit(1)
		}
		if err = (&controllers.GCPClusterReconciler{
			Client: mgr.GetClient(),
			Log:    ctrl.Log.WithName("controllers").WithName("GCPCluster"),
		}).SetupWithManager(mgr, controller.Options{MaxConcurrentReconciles: gcpClusterConcurrency}); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "GCPCluster")
			os.Exit(1)
		}
		if feature.Gates.Enabled(capifeature.MachinePool) && feature.Gates.Enabled(feature.GKE) {
			if err = (&infrav1controllersexp.GCPManagedClusterReconciler{
				Client: mgr.GetClient(),
				Log:    ctrl.Log.WithName("controllers").WithName("GCPManagedCluster"),
			}).SetupWithManager(mgr, controller.Options{MaxConcurrentReconciles: gcpManagedClusterConcurrency}); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "GCPManagedCluster")
				os.Exit(1)
			}
			if err = (&infrav1controllersexp.GCPManagedControlPlaneReconciler{
				Client: mgr.GetClient(),
				Log:    ctrl.Log.WithName("controllers").WithName("GCPManagedControlPlane"),
			}).SetupWithManager(mgr, controller.Options{MaxConcurrentReconciles: gcpManagedControlPlaneConcurrency}); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "GCPManagedControlPlane")
				os.Exit(1)
			}
			if err = (&infrav1controllersexp.GCPManagedMachinePoolReconciler{
				Client: mgr.GetClient(),
				Log:    ctrl.Log.WithName("controllers").WithName("GCPManagedMachinePool"),
			}).SetupWithManager(mgr, controller.Options{MaxConcurrentReconciles: gcpManagedControlPlaneConcurrency}); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "GCPManagedMachinePool")
				os.Exit(1)
			}
		}
	} else {
		if err = (&infrav1.GCPMachineTemplate{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "GCPMachineTemplate")
			os.Exit(1)
		}
		if err = (&infrav1.GCPMachine{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "GCPMachine")
			os.Exit(1)
		}
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddReadyzCheck("ping", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to create ready check")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to create health check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
