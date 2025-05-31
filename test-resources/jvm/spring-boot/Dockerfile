# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

ARG JDK_VERSION=24

FROM openjdk:${JDK_VERSION}-jdk-bookworm

COPY . .
COPY .m2 /m2

RUN JAVA_HOME=$(which java | xargs dirname | xargs dirname) "./mvnw" "-gs" "/m2/settings.xml" package

ENTRYPOINT [ "java", "-jar", "/target/app.jar" ]