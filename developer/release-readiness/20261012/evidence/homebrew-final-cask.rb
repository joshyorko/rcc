cask "rcc" do
  version "18.19.6"

  livecheck do
    skip "Updated by the tap's GitHub Actions workflow."
  end

  on_macos do
    on_arm do
      sha256 "18bfdd6c85abf5f26170a8e933c97799c884a067877943e65c74d4eadeb1e08f"
      url "https://github.com/joshyorko/homebrew-tools/releases/download/rcc-18.19.6/rcc-macosarm64"
      binary "rcc-macosarm64", target: "rcc"
    end

    on_intel do
      sha256 "22832fc7c8511f7827ea83cf19fcccb6265b57db986fccb07bf801a52c6078ce"
      url "https://github.com/joshyorko/homebrew-tools/releases/download/rcc-18.19.6/rcc-macos64"
      binary "rcc-macos64", target: "rcc"
    end
  end

  on_linux do
    sha256 "2036b84ec8637e76ea41ab90abd2963357b1fed5ceb9a4e58fe852c67baaff1e"
    url "https://github.com/joshyorko/homebrew-tools/releases/download/rcc-18.19.6/rcc-linux64"
    binary "rcc-linux64", target: "rcc"
  end

  name "RCC"
  desc "RCC - Repeatable Contained Code automation runtime"
  homepage "https://github.com/joshyorko/rcc"

  caveats <<~EOS
    If 'rcc' is not found after installation, refresh your shell's cache:
      hash -r

    Or start a new terminal session.
  EOS
end
