{
  description = "Kiro Gateway - Proxy for Amazon Q/CodeWhisperer API";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages.kiro-go-gw = pkgs.buildGoApplication {
          pname = "kiro-go-gw";
          version = "0.1.0";
          src = ./.;
          modules = ./go.mod;
          vendorHash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";
        };

        defaultPackage = self.packages.${system}.kiro-go-gw;
      }
    );
}
