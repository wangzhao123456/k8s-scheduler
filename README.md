# Batch-aware Kubernetes scheduler

This repository contains a production-ready custom scheduler that adds **batch/gang scheduling** to Kubernetes. Pods that share the same batch annotations will either all start together or all stay pending, avoiding partial scheduling that can deadlock distributed jobs.

## How it works

The scheduler is built on the upstream scheduler framework and ships a Permit plugin named `BatchPermit`.

* Pods opt in to batch scheduling using annotations:
  * `batch.scheduling.k8s.io/group`: logical batch name shared by all pods in the gang.
  * `batch.scheduling.k8s.io/min-available`: minimum number of pods that must be ready to start the batch.
* While the gang size is not satisfied, pods remain in the Permit phase and are not bound to nodes.
* When enough peers are waiting, the scheduler releases the entire group, ensuring all pods land together.

## Build the scheduler image

1. Build the binary locally:
   ```bash
   go mod tidy
   go build -o bin/batch-scheduler ./cmd/scheduler
   ```
2. Build and push an image to a registry your cluster can pull from:
   ```bash
   IMAGE=your-registry/batch-scheduler:latest
   docker build -t "$IMAGE" .
   docker push "$IMAGE"
   ```

## Deploy into a cluster

1. Update the image reference in [`cmd/scheduler/deploy/scheduler.yaml`](cmd/scheduler/deploy/scheduler.yaml) to the image you pushed.
2. Apply the deployment manifest (creates namespace, RBAC, config, and deployment):
   ```bash
   kubectl apply -f cmd/scheduler/deploy/scheduler.yaml
   ```
3. Verify the scheduler pod is running and leader election completed:
   ```bash
   kubectl -n batch-scheduler get pods
   kubectl -n batch-scheduler logs -f deploy/batch-scheduler
   ```

## Schedule a batch workload

Create a workload that references the custom scheduler name `batch-scheduler` and includes the batch annotations.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gang-worker-0
  namespace: default
  annotations:
    batch.scheduling.k8s.io/group: demo
    batch.scheduling.k8s.io/min-available: "3"
spec:
  schedulerName: batch-scheduler
  containers:
    - name: pause
      image: registry.k8s.io/pause:3.9
---
apiVersion: v1
kind: Pod
metadata:
  name: gang-worker-1
  namespace: default
  annotations:
    batch.scheduling.k8s.io/group: demo
    batch.scheduling.k8s.io/min-available: "3"
spec:
  schedulerName: batch-scheduler
  containers:
    - name: pause
      image: registry.k8s.io/pause:3.9
---
apiVersion: v1
kind: Pod
metadata:
  name: gang-worker-2
  namespace: default
  annotations:
    batch.scheduling.k8s.io/group: demo
    batch.scheduling.k8s.io/min-available: "3"
spec:
  schedulerName: batch-scheduler
  containers:
    - name: pause
      image: registry.k8s.io/pause:3.9
```

Apply the manifest and watch scheduling behavior:

```bash
kubectl apply -f gang-demo.yaml
watch kubectl get pods -l batch.scheduling.k8s.io/group=demo
```

All pods will stay in the `Pending` state until all three are admitted. Once the group is complete, they will transition to `Running` together.

## Operational tips

* The Permit timeout defaults to 10 minutes. Pods waiting longer will time out; adjust by rebuilding if you need a different value.
* Logs include the group key, waiting count, and release events to help troubleshoot admission decisions.
* To avoid starving other workloads, keep gang sizes aligned with cluster capacity.

## Push this repository to your GitHub

If you cloned this code from a different source and want to publish it under your GitHub account:

1. Create an empty repository on GitHub (e.g., `https://github.com/<you>/k8s-scheduler`). Do **not** initialize it with files.
2. Add your GitHub repo as a new remote and push the existing history:
   ```bash
   git remote add origin git@github.com:<you>/k8s-scheduler.git   # or use https://github.com/<you>/k8s-scheduler.git
   git push -u origin main
   ```
   Replace `main` with your current branch name if different.
3. Confirm the code is visible on GitHub:
   ```bash
   git remote -v
   git branch -vv
   ```
   Then browse to your repository URL.
