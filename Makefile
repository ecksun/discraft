NAME=discraft
VERSION=$(shell git describe --always --match v[0-9]* HEAD)
SHORT_VERSION=$(patsubst v%,%,$(VERSION))

OUT_DIR=./build
DEB_PACKAGE_DIR=$(OUT_DIR)/deb/$(NAME)-$(VERSION)

.PHONY: all
all: $(DEB_PACKAGE_DIR).deb

$(OUT_DIR):; mkdir -p $@

$(OUT_DIR)/discraft: $(shell find . -type f -iname "*.go") go.mod go.sum | $(OUT_DIR)
	go build -o $@

.PHONY: deb
deb: $(DEB_PACKAGE_DIR).deb
$(DEB_PACKAGE_DIR).deb: $(DEB_PACKAGE_DIR)/
	chmod 755 $(DEB_PACKAGE_DIR)/DEBIAN/postinst
	chmod 755 $(DEB_PACKAGE_DIR)/DEBIAN/postrm
	chmod 755 $(DEB_PACKAGE_DIR)/DEBIAN/prerm
	fakeroot dpkg-deb --build "$(DEB_PACKAGE_DIR)"

$(DEB_PACKAGE_DIR)/: \
	$(DEB_PACKAGE_DIR)/DEBIAN/ \
	$(DEB_PACKAGE_DIR)/usr/bin/$(NAME) \

	touch "$@"

$(DEB_PACKAGE_DIR)/DEBIAN/: \
	$(DEB_PACKAGE_DIR)/DEBIAN/conffile \
	$(DEB_PACKAGE_DIR)/DEBIAN/control \
	$(DEB_PACKAGE_DIR)/DEBIAN/postinst \
	$(DEB_PACKAGE_DIR)/DEBIAN/postrm \
	$(DEB_PACKAGE_DIR)/DEBIAN/prerm \

	touch "$@"

$(DEB_PACKAGE_DIR)/DEBIAN/control: debian/control
	(cat debian/control && echo -n 'Version: ' && echo "$(SHORT_VERSION)") > "$@"

$(DEB_PACKAGE_DIR)/DEBIAN/%: debian/%
	mkdir -p "$(dir $@)"
	cp -p "debian/$*" "$@"

$(DEB_PACKAGE_DIR)/usr/bin/$(NAME): $(OUT_DIR)/discraft
%/usr/bin/$(NAME):
	mkdir -p "$(dir $@)"
	cp --link "$<" "$@"
