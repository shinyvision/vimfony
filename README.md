<p align="center">
    <img src="https://raw.githubusercontent.com/shinyvision/vimfony/main/.github/assets/vimfony_sm.png" alt="Vimfony Logo">
</p>

This is a language server that adds Symfony integration in Neovim. It is the missing **Vim / Neovim Symfony plugin**. (totally added that for SEO reasons)

If you find a bug or want a feature, you can create an issue or a PR. I’ll probably take a look at it.
We finally have several autocomplete features working in Vimfony!

## Features
- `gd` Twig templates with @Bundle support
- `gd` Twig functions
- `gd` class from within yaml / xml files
- `gd` service definitions for example @service_container
- Autocomplete service names (works in yaml, xml and autoconfigure php attributes)
- Autocomplete Twig functions
- Autocomplete Twig variables
- Autocomplete route names and parameters in Twig files and PHP files
- Support for Composer’s autoload_classmap for more complete autoloading
- Support for multiple xml files in container_xml_path, for example in [Sulu](https://github.com/sulu/sulu) projects
- Autocomplete Twig files: works in php, twig and yaml (if the key is equal to ‘template’)
- `gd` routes
- Autocomplete translations (only YAML)
- `gd` translations (only YAML)

## Planned features
These features are not yet implemented but would be useful:
(feel free to create a PR if you want to contribute)
- Autocomplete form options
- `gd` Twig components
- Version checker & updater (`vimfony update`)

### Coming up
Main should be up-to-date with the latest release, so you’re good to go! 🥳

## How to use
You can [download a release](https://github.com/shinyvision/vimfony/releases) for your OS and CPU or build from source:
```bash
git clone https://github.com/shinyvision/vimfony.git
cd vimfony
go build
```

And then move the `vimfony` binary to somewhere in your $PATH

Configure LSP (Neovim):
```lua
local git_root = vim.fs.root(0, ".git")

if git_root ~= nil then
  vim.lsp.config('vimfony', {
    cmd = { "vimfony" },
    filetypes = { "php", "twig", "yaml", "xml" }, -- You can remove file types if you don't like it, but then it won't work in those files
    root_markers = { ".git" },
    single_file_support = true,
    init_options = {
      roots = { "templates" },
      container_xml_path = (git_root .. "/var/cache/dev/App_KernelDevDebugContainer.xml"),
      -- OR:
      -- container_xml_path = {
      --   (git_root .. "/var/cache/dev/App_KernelDevDebugContainer.xml"),
      --   (git_root .. "/var/cache/website/dev/App_KernelDevDebugContainer.xml"),
      --   (git_root .. "/var/cache/admin/dev/App_KernelDevDebugContainer.xml"),
      -- },
      vendor_dir = git_root .. "/vendor",
      -- Optional:
      -- php_path = "/usr/bin/php",
    },
  })
  vim.lsp.enable('vimfony')
end
```

If you use this project and like what it does, then please **give it a star** on Github.

PS. I highly recommend purchasing a license for [Intelephense](https://intelephense.com/). It’s worth your 25 bucks.

## Finally
I have no idea if this works for VSCode. I have never used VSCode, but I’ve heard it uses language servers in the same way as Neovim. Maybe it’ll work, maybe it won’t.
