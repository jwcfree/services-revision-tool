# CHANGELOG

## 0.0.7 / 2023.12.15

- [feature/PGSDEVOPS-3143](https://jira.egovdev.ru/browse/PGSDEVOPS-3143) Добавлен общий список зависимостей в итоговый отчет и обработка сервисов в имени которых есть пробелы.

## 0.0.6 / 2023.11.24

- [master](https://jira.egovdev.ru/browse/PGSDEVOPS-3002) Добавлена сборка с помощью docker, определение версии gradle, исправлена Readme.tmpl, выгрузка докер образа для сборки и конфигов gradle.

## 0.0.5 / 2023.09.04

- [feature/PGSDEVOPS-2742](https://jira.egovdev.ru/browse/PGSDEVOPS-2742) Добавлена поддержка socks5 прокси, с авторизацияей и без, для работы в закрытом контуре

## 0.0.4 / 2023.08.29

- [PGSDEVOPS-2689](https://jira.egovdev.ru/browse/PGSDEVOPS-2689) Добавлен рассчет хеш-суммы для архивов и файлов зависимостей по ГОСТ алгоритму

## 0.0.3 / 2023.08.14

- [feature/PGSDEVOPS-2614](https://jira.egovdev.ru/browse/PGSDEVOPS-2614) Добавлена поддержка кеша для исключения повторного скачивания одних и тех же зависимостей и исходных кодов из Maven Central и gitlab

## 0.0.2 / 2023.08.08

- [feature/PGSDEVOPS-2563](https://jira.egovdev.ru/browse/PGSDEVOPS-2563) Добавлен обход блокировок со стороны репозитория Maven Central при многопоточном скачивании. Плюс добавлена подстановка nexus репозитория в build.gradle.

## 0.0.1 / 2023.07.25

- [PGSDEVOPS-2338](https://jira.egovdev.ru/browse/PGSDEVOPS-2338) Первая версия утилиты
