pluginManagement {
    repositories {
        google()
        mavenCentral()
        // Read-only public mirror for transient Maven Central access failures on CI.
        maven("https://maven-central.storage-download.googleapis.com/maven2")
        gradlePluginPortal()
    }
}
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
        maven("https://maven-central.storage-download.googleapis.com/maven2")
    }
}
rootProject.name = "GoreeCloudBackup"
include(":app")
