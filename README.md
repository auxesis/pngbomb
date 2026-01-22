# pngbomb

Go implementation of the [pngbomb](https://github.com/liclac/pngbomb/) technique as implemented by @liclac.

## Using

Generate a malicious PNG by running:

```bash
pngbomb
```

This outputs a PNG at `image.png`.

## Developing

### Setup

Ensure you have git and [mise](https://mise.jdx.dev/) installed, then run:

```bash
# clone the repo
git clone https://github.com/auxesis/pngbomb
cd pngbomb

# install dependencies
mise install
```

### Run

Run `pngbomb.go`:

```bash
mise run dev
```

This outputs a 10,000x10,000px 1-bit greyscale PNG at `./image.png`.

Inspect it with `file` and `ls`:

```bash
file image.png
ls -lah image.png
```

### Build

Build a standalone binary:

```bash
mise run build
```

This outputs a binary at `./pngbomb`.

### Test

Run the test suite:

```bash
mise run test
```
