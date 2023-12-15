# services-revision-tool

Утилита сбора исходных кодов сервисов и их зависимостей

## Список изменений

Все значимые изменения можно найти в файле [CHANGELOG.md](CHANGELOG.md).

## Требования

Для сборки, установки и использования утилиты требуются следующие программные средства:

- go >= v1.19
- cpverify от Криптопро

При создании документации используются дополнительные программы:

- [markdownlint](https://github.com/DavidAnson/markdownlint),
- [markdown-link-check](https://github.com/tcort/markdown-link-check).

Для работы утилиты требуется наличие переменной среды `GIT_TOKEN` с правами на группы и репозитории из конфига, а также `NEXUS_USER` и `NEXUS_PASS` для загрузки архивов в Nexus. Если переменные пустые приложение выведе ошибку.

Так же требуется наличие docker на машине, с которой происходит запуск приложение. Это нужно для запуска сборки сервиса и получения всех зависимостей.

## Разработка

Приложение написано для сбора исходных кодов сервисов и их зависимостей, работает в любой linux среде.

### Makefile

Для управления жизненным циклом используется хорошо документированный [Makefile](Makefile). Краткую справку по доступным целям `make` можно получить, выполнив команду `make help` или просто `make`.

```sh

Usage:
  make <target>

General
  help             Display this help.

Development
  fmt              Run go fmt against code.
  vet              Run go vet against code.

Build
  build            Build manager binary.
  mod              Update go.mod file.
  vendor           Update vendored packages.
  doc-check        Lint documentation and check links.
  doc-lint         Check documentation via markdownlint.
  doc-check-links  Check documentation links via markdown-link-check.
  major            Release new major version: make major release.
  minor            Release new minor version: make minor release.
  patch            Release new patch version: make patch release.
  release          Release new version: make ?type? release.

```

### Версионирование

Утилита версионируется согласно [Semver 2.0](https://semver.org/). Версия утилиты содержится в файле [VERSION](VERSION). Содержимое файла внедряется в исполнимый файл на этапе сборки и используется в CI/CD для маркировки артефактов.

### Сборка

Для сборки оператора можно использовать команду `make build`, которая создаст манифесты, запустит `go fmt` для форматирования исходного кода, `go vet` для статической проверки кода, `go test` для тестирования и `go build` для сборки приложения. Собранный файл будет сохранён под именем `bin/gitlab-baseimages-tool`.

### Выпуск новой версии утилиты

Для создания новой версии утилиты нужно выполнить следующие действия:

- Перечислить произведённые изменения в файле [CHANGELOG.md](CHANGELOG.md).
- Установить новую версию оператора в файле [VERSION](VERSION).

Для упрощения перечисленных действий создана отдельная цель
[Makefile](Makefile): `release`, которая может автоматически поднимать версию
(с учётом семантического версионирования) и выполнять рутинные действия по
частичному заполнению [CHANGELOG.md](CHANGELOG.md),
учитывая текущую ветку, названную по задаче в Jira:

```sh
# Создать выпуск с версией из файла VERSION.
$ make release

# Создать новый мажорный выпуск.
$ make major release

# Создать новый минорный выпуск.
$ make minor release

# Создать новый рядовой выпуск.
$ make patch release
```

## Использование утилиты

### Аргументы командной строки

```sh
Usage of ./sevices-revision-tool:
  -configfile string
        Path to json config file (default "config.json")
  -force
        Force replace temp work files
  -loglevel string
        Log level, could be WARN, DEBUG, TRACE, ERROR (default "INFO")
  -v    Show version
```

## Описание утилиты

### Конфигурационный файл

По-умолчанию файл находится в той-же папке что и программа.

Пример:

```json

{
"gitlab_api_host" : "https://git.gosuslugi.local/api/v4",
"output_dir" : "result",
"service_list" : [
        "idm"
],
"group_id": "1742",
"branch":"dev",
"cache":true,
"cache_dir": "cache",
"archive_format":"tgz",
"maven_url": "https://repo1.maven.org/maven2",
"plugins_url": "https://plugins.gradle.org/m2",
"max_parallelism": 2,
"readme_template":"readme.md.tmpl",
"rtl_search_repo_id": "2706",
"upload_to_nexus": true,
"nexus_url":"http://10.65.133.229:8081",
"nexus_path":"/repository/pgs2-sources/services",
"nexus_maven_url": "http://10.65.133.229:8081/repository/maven-public/",
"nexus_force_add_to_gradle": false,
"nexus_auto_add_to_gradle": true,
"proxy":false,
"proxy_host": "127.0.0.1",
"proxy_port": "1080",
"proxy_user": "nil",
"proxy_pass": "nil"

}

```

`gitlab_api_host` - адрес api gitlab

`output_dir` - каталог для сохранения результатов работы

`service_list` - массим с именами сервисов, которые обрабатываем. Должен совпадать в именем проекта в gitlab

`group_id` - ID группы в gitlab, откуда берем исходыне коды сервисов

`cache` - Признак использования кеша каталога для зависимостей и исходныз кодов. Кеш один на все обрабатываемые сервисы.

`cache_dir` - каталог для кеша.

`branch` - ветка сервисов, из которой берем исходные коды

`archive_format` - формат архива с исходными кодами. Список [тут](https://docs.gitlab.com/ee/api/repositories.html#get-file-archive)

`maven_url` - адрес основого репозитория Maven, где ищем зависимости и их исходники

`plugins_url` - адрес репозитория плагинов для gradle.

`max_parallelism` - максимальное количество одновременно обрабатываемых сервисов.

`readme_template` - пусть к файлу с шаблоном документации сервиса и инструкицями для сборки

`rtl_search_repo_id` - id группы gitlab, в которой ищем зависимости собственной разработки РТЛабс

`upload_to_nexus` - признак загрузки результата в Nexus репозиторий

`nexus_url` - адрес Nexus

`nexus_path` - путь до репозитория

`nexus_maven_url` - путь до репозитория с локальными зависомостями для сборки.

`nexus_force_add_to_gradle` - признак принудительного добавления в build.gradle локального nexus и maven репозитория. Для сборки и использования зависимостей собственной разработки

`nexus_auto_add_to_gradle` - признак автоматического добавления в build.gradle локального nexus и maven репозитория. Если без него произошла ошибка сборки, произойдет добавление репозитория и сборка повторится.

`proxy` - Признак использования socks5 прокси сервера
`proxy_host` -  Адрес прокси сервера
`proxy_port` - Порт прокси сервера
`proxy_user` - Пользователь прокси сервера, если авторизация не использует должно быть значение "nil"
`proxy_pass` - Пароль от прокси сервера, если авторизация не используется должно быть значение "nil"

### Логика работы

Приложение получает проекты из Gitlab, далее циклом (с учетом многопоточности) обрабатывает список сервисов: получает архив исходников через gitlab SDK, парсит из файла `Dockerfile.pgs2` команду сборки, запускает сборку (с учетом добавления локального Nexus в build.gradle, зависит от конфига), по списку библиотек из кеша gradle скачивает их с репозиториев maven central или plugins. Если зависимость имеет префикс `sx.microservices` или `rtl` то исходники скачиваются из gitlab. Далее все вносится в Readme.md файл, упаковывается (исходники, кеш gradle и зависимости) и загружается в Nexus (если активна такая опция). Результат работы сохраняется локально в папке, указанной в конфиге. Сборка сервиса производится с помощью docker образа из Dockerfile.pgs2, сам образ выгружается в итоговый архив с исходниками сервиса.

У приложения есть разный уровень вывода логов, возможность перезаписи папки с результатами, обработка всех ошибок.

В итоговой папке появится архив сервиса со всеми зависимостями и исходниками и общий для всех сервисов файл `report.txt` со списком не найденных зависимостей и зависимостей без исходных кодов.

В приложение добавлен функционал кешировния зависимостей для исключения повторного скачивания одного и того же исходного кода. Кеш общий на все обрабатываемые сервисы и может использоваться многократно. Кешируются не только бибилотеки из Maven Central, но и исходные коды из gitlab. Т.е. если для одного из сервисов библиотек уже скачана, то для следующего она просто копируется из папки с кешем.

Для каждого архива .tgz, включая итоговый автоматически рассчитывается хеш-сумма утилитой cpverify от Криптопро, складывается в одноименный файл с расширением .gost.

Для работы в закрытом контуре добавлена поддержка socks5 прокси сервера.


### Пример запуска

`./sevices-revision-tool -force -loglevel TRACE`

### TODO

Сделать тесты
