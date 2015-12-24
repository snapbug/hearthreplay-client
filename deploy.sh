#!/bin/bash
set -e

ROOT="hearthreplay-client"

function doclient() {
	j="../server/tmpl/client.$1.$2.json"
	p="out/$3/${ROOT}-$1-$2-$3"
	checksum=$(openssl dgst -sha256 ${p} | sed 's/^.* //')

	signature=$(openssl dgst -sha256 -sign private.pem -keyform PEM ${p} | base64)

	echo "{\"version\": \"$3\", \"checksum\": \"${checksum}\", \"signature\":\"${signature}\"}" >| ${j}

	git add ${j}
}

# get the git commit hash
SHA=$(git log --pretty=format:"%h" -n 1)

echo Generating bindata.go
go-bindata tmpl/

echo Building version: ${SHA}

mkdir out/${SHA}

env=windows
for arch in amd64 386
do
	echo Building client for ${env} ${arch}
	GOOS=${env} GOARCH=${arch} godep go build -o out/${SHA}/${ROOT}-${env}-${arch}-${SHA} -ldflags "-s -X main.Version=${SHA}" client.go bindata.go
	doclient ${env} ${arch} ${SHA}
done

env=darwin
arch=amd64
echo Building client for ${env} ${arch}
GOOS=${env} GOARCH=${arch} godep go build -o out/${SHA}/${ROOT}-${env}-${arch}-${SHA} -ldflags "-s -X main.Version=${SHA}" client.go bindata.go
doclient ${env} ${arch} ${SHA}

echo Updating version to s3
 aws s3 sync out/${SHA} s3://update.hearthreplay.com --acl public-read

git commit -m "Updating client version"

echo git push and pull on server to finish deploy
