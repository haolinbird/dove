BUILD_DIR=$(PWD)/build/
INSTALL_DIR=/usr/local/DoveClient/
BUILD_CMD=CGO_ENABLED=0 go build -installsuffix cgo -a -ldflags "-w -X main.etcdclientVer=`cd vendor/github.com/coreos/etcd && git rev-parse HEAD`" -o $(BUILD_DIR)DoveClient
LPWD=$(PWD)
BEFORE_MAKE=before-make
POST_MAKE=post-make
BUILD_TARGET=build-target
default: before-make build-target post-make post-make-msg

nodep: init-settings build-target post-make post-make-msg

linux: $(BEFORE_MAKE) build-target-linux $(POST_MAKE) post-make-msg
linux386: $(BEFORE_MAKE) build-target-linux386 $(POST_MAKE)  post-make-msg

darwin: $(BEFORE_MAKE) build-target-darwin $(POST_MAKE)   post-make-msg
clean:
	rm -rf $(BUILD_DIR)
	rm -f $(LPWD)/config/settings.go
	rm -f $(BUILD_DIR)/settings.pack.toml.go

settings: before-make

post-make:
	@echo "Building complete\n"
	@rm  $(LPWD)/config/settings.go
	@rm $(BUILD_DIR)/settings.pack.toml.go


post-make-msg:
	@echo "doveclient is located as '$(BUILD_DIR)/DoveClient'\n"
	@echo "donot forget to remove your secret settings"

install:default
	sudo mkdir -p $(INSTALL_DIR)
	sudo cp $(BUILD_DIR)/DoveClient $(INSTALL_DIR)

zippack: before-make build-zippack post-make
zipwin: before-make build-zipwin post-make

build-zipwin:
	mkdir -p $(BUILD_DIR)DoveClient-win
	GOOS=windows GOARCH=386 CGO_ENABLED=0 go build -installsuffix cgo -a -ldflags "-w" -o $(BUILD_DIR)DoveClient-win/DoveClient.exe
	cd $(BUILD_DIR) && cp $(LPWD)/scripts/etc/DoveClient.conf DoveClient-win/config.ini && zip -r -9 DoveClient-win.zip DoveClient-win/*
	@echo "zipwin successful"
build-zippack:
	@$(LPWD)/scripts/create_deploy.sh


build-target:
	@echo "Building..."
	$(BUILD_CMD)

build-target-darwin:
	@echo "Building..."
	cd $(LPWD) && GOOS=darwin  $(BUILD_CMD)

build-target-linux:
	@echo "Building..."
	cd $(LPWD) && GOOS=linux $(BUILD_CMD)

build-target-linux386:
	@echo "Building..."
	cd $(LPWD) && GOOS=linux GOARCH=386  $(BUILD_CMD)

check-deps:
	@echo "Checking dependencies" 
	@go get github.com/tools/godep 
#	godep restore

init-settings:
	@if test ! -e "$(BUILD_DIR)"; then\
		mkdir $(BUILD_DIR);\
        fi; 
	@echo "Creating secret settings..."
	@echo "package build\nconst SecretSettingsRaw = \`" > $(BUILD_DIR)/settings.pack.toml.go 
	@cat settings.pack.toml >> $(BUILD_DIR)/settings.pack.toml.go
	@echo "\`" >> $(BUILD_DIR)/settings.pack.toml.go
	@echo "Building secret settings..."
	@cd $(LPWD)/scripts &&\
	go build create_settings.go &&\
	./create_settings && rm create_settings 

before-make: check-deps init-settings
	
help:
	@echo "Available extra make rules are as below";
	@echo "linux            build a linux version"
	@echo "linux386         build a linux 32bit version"
	@echo "darwin           build a macosx version"
	@echo "zippack          build a zip pack for installing via DoveServer api (linux/darwin platform only)"
	@echo "zipwin           build a zip pack for windows platform"
	@echo "settings         create setting files for building project only."
	@echo "nodep            do not check dependencies, and build default target"


