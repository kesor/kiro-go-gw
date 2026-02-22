{
  description = "Kiro Gateway - Proxy for Amazon Q/CodeWhisperer API";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    kiro-go-gw-src.src = ./.;
    kiro-go-gw-src.flake = false;
  };

  outputs =
    {
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
          src = self;
          vendorHash = pkgs.lib.fakeHash;
          subPackages = [ "cmd/server" ];
          buildInputs = [ pkgs.sqlite ];
          nativeBuildInputs = [ pkgs.pkg-config ];
          CGO_ENABLED = 1;
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
