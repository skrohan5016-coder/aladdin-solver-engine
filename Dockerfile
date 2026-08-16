FROM golang:1.24.13-alpine3.22@sha256:3641e0d9b931dc4f2f185dcd669c4679670e9277c8166a838ddb98a2d4389cb5 AS build

ENV GOTOOLCHAIN=local
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/solver ./cmd/solver \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/report ./cmd/report

FROM scratch
COPY --from=build /out/solver /solver
COPY --from=build /out/report /report
EXPOSE 8000
USER 65532:65532
ENTRYPOINT ["/solver"]
