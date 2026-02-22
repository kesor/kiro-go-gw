{
  description = "Kiro Gateway - Proxy for Amazon Q/CodeWhisperer API";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils?rev=11707dc2f618dd54ca8739b309ec4fc024de578b";
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
        packages.kiro-go-gw = pkgs.callPackage ./package.nix {
          src = self;
          vendorHash = "sha256-3fikKiBH4RnUMg9xtr3MNBaxTNObGMAVbaXh4WBQne8=";
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
