#/bin/bash
echo "building container"

GOOS=linux GOARCH=amd64 go build -o ./bin/resume ./src/*.go

# docker build -t rogierlommers/resume .
# docker push rogierlommers/resume:latest
