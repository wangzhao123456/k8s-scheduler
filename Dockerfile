FROM golang:1.21 AS builder
WORKDIR /workspace
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/kube-scheduler ./cmd/scheduler

FROM gcr.io/distroless/base-debian12:nonroot
COPY --from=builder /out/kube-scheduler /bin/kube-scheduler
USER nonroot:nonroot
ENTRYPOINT ["/bin/kube-scheduler"]
