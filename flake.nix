{
  description = "Kiro Gateway - Proxy for Amazon Q/CodeWhisperer API";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    kiro-go-gw-src.url = "github:kesor/kiro-go-gw";
    kiro-go-gw-src.flake = false;
  };

  outputs =
    inputs@{
      self,
      nixpkgs,
      flake-utils,
      kiro-go-gw-src,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages.kiro-go-gw = pkgs.buildGoModule {
          pname = "kiro-go-gw";
          version = "0.1.0";
          src = kiro-go-gw-src;
          vendorHash = pkgs.lib.fakeHash;
          subPackages = [ "cmd/server" ];
          buildInputs = [ pkgs.sqlite ];
          nativeBuildInputs = [ pkgs.pkg-config ];
        };

        packages.default = self.packages.${system}.kiro-go-gw;

        devShells.default = pkgs.mkShell {
          buildInputs = [
            pkgs.go
            pkgs.sqlite
            pkgs.pkg-config
          ];
        };
      }
    );
}
