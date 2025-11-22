package main

import (
	"fmt"
	"os"

	"k8s.io/klog/v2"
	schedulerapp "k8s.io/kubernetes/cmd/kube-scheduler/app"

	"k8s-scheduler/pkg/plugins/batchpermit"
)

func main() {
	cmd := schedulerapp.NewSchedulerCommand(
		schedulerapp.WithPlugin(batchpermit.Name, batchpermit.BuildConfig()),
	)

	if err := cmd.Execute(); err != nil {
		klog.ErrorS(err, "scheduler exited with error")
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
