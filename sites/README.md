# Local Test Sites — Quick Reference

All commands assume `gopress` has been installed onto `$PATH` via `make install`. If not installed, substitute `./build/gopress serve ...`.

## Auto-discover the first site under sites/
./build/gopress serve

## Run with a specific site config
./build/gopress serve -config local-test/hurricane-techs/localhost/config.toml

## Switch to the civic-estate theme
./build/gopress serve -config local-test/civic-estate/localhost/config.toml

## Switch to the FloraFi theme
./build/gopress serve -config local-test/florafi/localhost/config.toml

## Switch to the Axis Form theme
./build/gopress serve -config local-test/axis-form/localhost/config.toml

## Switch to the atelier-slate theme
./build/gopress serve -config local-test/atelier-slate/localhost/config.toml

## Switch to the terra-trail theme
./build/gopress serve -config local-test/terra-trail/localhost/config.toml
