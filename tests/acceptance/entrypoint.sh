#!/usr/bin/env sh

res=0

echo "Run servicecatalog tests"
./servicecatalog.test -test.v
res=$((res+$?))

echo "Run application tests"
./application.test -test.v
res=$((res+$?))

exit ${res}
