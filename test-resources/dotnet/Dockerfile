# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

FROM mcr.microsoft.com/dotnet/sdk:9.0-bookworm-slim AS build

RUN mkdir /App
WORKDIR /App
COPY . .

# run dotnet restore as a separate image layer (speeds up docker build)
RUN dotnet restore
# build application
RUN dotnet publish -o .

# build the final image
FROM mcr.microsoft.com/dotnet/aspnet:9.0-bookworm-slim

COPY --from=build /App /App
WORKDIR /App/
ENTRYPOINT [ "./app" ]
