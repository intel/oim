This directory contains several unmodified .proto files or (whenever
possible) symlinks to those files in the top-level vendor directory
managed by dep.

These files are needed because we want to re-generate all .pb.go files
with gogo/genproto also in those cases where upstream decided to stick
with golang/genproto. We do this mostly for the sake of consistency
and to reduce the overall number of dependencies. Performance of
gogo/genproto is said to be better, but that does not matter that much
for OIM.
