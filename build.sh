#/bin/bash
echo "building container"

# build binary
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ./bin/resume ./src/*.go

# build container and push to registry
docker build -t rogierlommers/resume .
docker push rogierlommers/resume:latest
