{
  description = "Kiro Gateway - Proxy for Amazon Q/CodeWhisperer API";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    src.url = "github:kesor/kiro-go-gw";
    src.flake = false;
  };

  outputs =
    inputs@{
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        go = pkgs.go_1_23;
      in
      {
        packages.kiro-go-gw = pkgs.buildGoApplication {
          inherit (inputs) src go;
          pname = "kiro-go-gw";
          version = "0.1.0";
          subPackages = [ "cmd/server" ];
          CGO_ENABLED = 1;
          buildInputs = [ pkgs.sqlite ];
          nativeBuildInputs = [ pkgs.pkg-config ];
        };

        packages.default = self.packages.${system}.kiro-go-gw;

        devShells.default = pkgs.mkShell {
          buildInputs = [
            go
            pkgs.sqlite
            pkgs.pkg-config
          ];
        };
      }
    );
}
