# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

ARG base_image_build=mcr.microsoft.com/dotnet/sdk:10.0-resolute
ARG base_image_run=mcr.microsoft.com/dotnet/aspnet:10.0-resolute

FROM ${base_image_build} AS build
ARG base_image_build

RUN mkdir /App
WORKDIR /App
COPY . .

# adapt the target framework of the application to the .NET version of the build base image
RUN case "${base_image_build}" in \
    *:8.0*) target_framework=net8.0 ;; \
    *:9.0*) target_framework=net9.0 ;; \
    *) target_framework= ;; \
  esac && \
  if [ -n "${target_framework}" ]; then \
    sed -i "s|<TargetFramework>net10.0</TargetFramework>|<TargetFramework>${target_framework}</TargetFramework>|" app.csproj \
  ; fi

# run dotnet restore as a separate image layer (speeds up docker build)
RUN dotnet restore
# build application
RUN dotnet publish -o .

# build the final image
FROM ${base_image_run}

COPY --from=build /App /App
WORKDIR /App/
ENTRYPOINT [ "./app" ]
