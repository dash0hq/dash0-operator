# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

CFLAGS ?= -Wall -Werror -O2
PREFIX = /usr
LIBDIR = $(PREFIX)/lib
BINDIR = $(PREFIX)/bin

all: libdash0envhook.so appundertest.so

libdash0envhook.so: libdash0envhook.c
	$(CC) $(CFLAGS) -fPIC -rdynamic -shared -ldl -o $@ libdash0envhook.c

appundertest.so: appundertest.c
	$(CC) $(CFLAGS) -o $@ appundertest.c

clean:
	@rm -f *.so

install:
	install -d $(DESTDIR)$(LIBDIR) $(DESTDIR)$(BINDIR)
	install -m 644 libdash0envhook.so $(DESTDIR)$(LIBDIR)/
	install -m 755 dash0-env-hook.sh $(DESTDIR)$(BINDIR)/

.PHONY: all clean install
