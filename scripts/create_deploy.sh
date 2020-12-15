#!/bin/bash
SCRIPT_DIR=$(cd "$(dirname "$0")"; pwd)/
PROJ_ROOT=$(cd $SCRIPT_DIR/../ && pwd)/
BUILD_DIR=$(cd $SCRIPT_DIR/../build/ && pwd)
destFile=$BUILD_DIR/DoveClient.zip
oldPwd=$SCRIPT_DIR
CGO_ENABLED=0
origGoarch=`go env GOARCH`
origGoos=`go env GOOS`
exec_file_linux=DoveClient.Linux
GOOS=linux GOARCH=386
export GOOS
export GOARCH

cd $PROJ_ROOT  && go build -a -ldflags "-w" -o $BUILD_DIR/$exec_file_linux

if [ $? -ne 0 ]; then
    exit 1
fi
exec_file_darwin=DoveClient.Darwin
GOOS=darwin GOARCH=amd64
export GOOS
export GOARCH

cd $PROJ_ROOT && go build -a -ldflags "-w" -o $BUILD_DIR/$exec_file_darwin
if [ $? -ne 0 ]; then
    exit 1
fi

GOOS=$origGoos GOARCH=$origGoarch
export GOOS
export GOARCH

cd $SCRIPT_DIR
tmpdir=/tmp/dovedeploy/DoveClient/
rm -rf $tmpdir
mkdir -p $tmpdir
mv $BUILD_DIR/$exec_file_linux $tmpdir/$exec_file_linux
mv $BUILD_DIR/$exec_file_darwin $tmpdir/$exec_file_darwin

cp api_install/deploy.sh $tmpdir
cp macos/com.jumei.doveclient.plist $tmpdir
cp macos/DoveClient.darwin.sh $tmpdir
cp -rf etc  $tmpdir/
cd $tmpdir/..
zip -r -9 DoveClient DoveClient && mv DoveClient.zip $destFile
cd $oldPwd
rm -rf $tempdir
echo "packed as $destFile"