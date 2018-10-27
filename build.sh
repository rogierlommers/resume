#/bin/bash
echo "building container"

GOOS=linux GOARCH=amd64 go build -o ${BUILD_DIR}/pingback-linux-amd64 .
docker build -t rogierlommers/resume .
docker push rogierlommers/resume:latest
