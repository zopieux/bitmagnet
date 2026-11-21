{
  description = "bitmagnet dev shell";
  inputs.nixpkgs.url = "https://channels.nixos.org/nixpkgs-unstable/nixexprs.tar.zst";
  inputs.flake-utils.url = "github:numtide/flake-utils";

  outputs =
    {
      nixpkgs,
      flake-utils,
      ...
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs {
          system = system;
        };
        # The SolidJS webui. webui/dist is gitignored, so the go package
        # below copies the built dist out of this derivation.
        webui = pkgs.buildNpmPackage {
          pname = "bitmagnet-webui";
          version = "0.0.0";
          src = ./webui;
          npmDepsHash = "sha256-fJbL1/MByofnZNIM9nLoobIMNbxfmeCGDLIxxymMb7w=";
          installPhase = ''
            runHook preInstall
            mkdir -p $out/share/bitmagnet-webui
            cp -r dist $out/share/bitmagnet-webui/dist
            runHook postInstall
          '';
        };
      in
      {
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
            doCheck = false;
          };
        };
        devShells = {
          default = pkgs.mkShell {
            packages =
              with pkgs;
              [
                curl
                go
                inotify-tools
                mprocs
                nodejs_24
                prettier
                protobuf
                protoc-gen-go
                sqlite
              ]
              ++ (
                if stdenv.hostPlatform.isLinux then
                  [
                    chromium
                    shot-scraper
                    playwright-driver.browsers
                  ]
                else
                  [ ]
              );
            shellHook = pkgs.lib.optionalString pkgs.stdenv.hostPlatform.isLinux ''
              export PLAYWRIGHT_BROWSERS_PATH="${pkgs.playwright-driver.browsers}"
            '';
          };
        };
      }
    );
}
