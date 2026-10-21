{
  description = "bitmagnet dev shell";
  inputs.nixpkgs.url = "github:nixos/nixpkgs?rev=e7439b6b14ad3cc35d05608ebca9bce01a25f5f8";
  inputs.flake-utils.url = "github:numtide/flake-utils";

  outputs = {
    nixpkgs,
    flake-utils,
    ...
  }:
    flake-utils.lib.eachDefaultSystem (system: let
      pkgs = import nixpkgs {
        system = system;
      };
      # The SolidJS webui. webui/dist is gitignored, so the go package
      # below copies the built dist out of this derivation.
      webui = pkgs.buildNpmPackage {
        pname = "bitmagnet-webui";
        version = "0.0.0";
        src = ./webui;
        npmDepsHash = "sha256-zXoW0ao+CvJsykmhGgM9mS1YVPX/Mgqs/iyp86EOuFU=";
        installPhase = ''
          runHook preInstall
          mkdir -p $out/share/bitmagnet-webui
          cp -r dist $out/share/bitmagnet-webui/dist
          runHook postInstall
        '';
      };
    in {
      packages = {
        inherit webui;
        default = pkgs.buildGoModule {
          pname = "bitmagnet";
          version = "0.0.0";
          src = ./.;
          vendorHash = "sha256-LPfWsnASmj647g+Wr1FATeyzGtdMJe9c4+coKKidZpM=";
          # webui.go embeds webui/dist; it is built, not committed.
          preBuild = ''
            cp -r ${webui}/share/bitmagnet-webui/dist webui/dist
          '';
        };
      };
      devShells = {
        default = pkgs.mkShell {
          packages = with pkgs;
            [
              go
              mprocs
              nodejs_24
              prettier
              protobuf
              protoc-gen-go
            ]
            ++ (
              if stdenv.hostPlatform.isLinux
              then [
                chromium
              ]
              else []
            );
        };
      };
    });
}
