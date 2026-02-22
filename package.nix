{
  buildGoModule,
  src,
  vendorHash,
  sqlite,
  pkg-config,
}:

buildGoModule {
  inherit src vendorHash;
  pname = "kiro-go-gw";
  version = "0.1.0";
  subPackages = [ "cmd/server" ];
  buildInputs = [ sqlite ];
  nativeBuildInputs = [ pkg-config ];
}
