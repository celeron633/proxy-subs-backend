.PHONY: all release clean

BINARY_NAME=proxy-subs-backend
BUILD_DIR=build
DIST_DIR=dist
RELEASE_FILE=$(DIST_DIR)/$(BINARY_NAME).tar.gz

all: clean build

build:
	mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) .

release: all
	mkdir -p $(DIST_DIR)/$(BINARY_NAME)
	cp $(BUILD_DIR)/$(BINARY_NAME) $(DIST_DIR)/$(BINARY_NAME)/
	cp -r static $(DIST_DIR)/$(BINARY_NAME)/
	cp -r config $(DIST_DIR)/$(BINARY_NAME)/
	cp start.sh $(DIST_DIR)/$(BINARY_NAME)/
	cp stop.sh $(DIST_DIR)/$(BINARY_NAME)/
	chmod +x $(DIST_DIR)/$(BINARY_NAME)/start.sh
	chmod +x $(DIST_DIR)/$(BINARY_NAME)/stop.sh
	cd $(DIST_DIR) && tar -czf $(BINARY_NAME).tar.gz $(BINARY_NAME)/
	rm -rf $(DIST_DIR)/$(BINARY_NAME)
	@echo "Release package created: $(RELEASE_FILE)"

clean:
	rm -rf $(BUILD_DIR)
	rm -rf $(DIST_DIR)
