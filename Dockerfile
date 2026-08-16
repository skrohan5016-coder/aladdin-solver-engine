FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/solver ./cmd/solver \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/report ./cmd/report

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/solver /solver
COPY --from=build /out/report /report
EXPOSE 8000
USER nonroot:nonroot
ENTRYPOINT ["/solver"]
