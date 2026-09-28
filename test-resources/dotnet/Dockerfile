# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

FROM mcr.microsoft.com/dotnet/sdk:10.0-resolute AS build

RUN mkdir /App
WORKDIR /App
COPY . .

# run dotnet restore as a separate image layer (speeds up docker build)
RUN dotnet restore
# build application
RUN dotnet publish -o .

# build the final image
FROM mcr.microsoft.com/dotnet/aspnet:10.0-resolute

COPY --from=build /App /App
WORKDIR /App/
ENTRYPOINT [ "./app" ]
