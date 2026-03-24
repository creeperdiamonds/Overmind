package com.overmind.java.plugin;

import java.util.Collections;
import java.util.List;

/**
 * Immutable metadata parsed from a plugin's {@code plugin.yml}.
 *
 * <p>Minimum required keys: {@code name}, {@code version}, {@code main}.
 */
public final class PluginMeta {

    private final String name;
    private final String version;
    private final String main;
    private final String description;
    private final String apiVersion;
    private final List<String> authors;
    private final List<String> depend;
    private final List<String> softdepend;

    public PluginMeta(String name, String version, String main,
                      String description, String apiVersion,
                      List<String> authors, List<String> depend, List<String> softdepend) {
        this.name        = name;
        this.version     = version;
        this.main        = main;
        this.description = description == null ? "" : description;
        this.apiVersion  = apiVersion  == null ? "" : apiVersion;
        this.authors     = Collections.unmodifiableList(authors);
        this.depend      = Collections.unmodifiableList(depend);
        this.softdepend  = Collections.unmodifiableList(softdepend);
    }

    /** The plugin's display name (e.g. {@code "MyPlugin"}). */
    public String getName()        { return name; }

    /** Semver-style version string from {@code plugin.yml}. */
    public String getVersion()     { return version; }

    /** Fully-qualified main class name (e.g. {@code "com.example.MyPlugin"}). */
    public String getMain()        { return main; }

    public String getDescription() { return description; }
    public String getApiVersion()  { return apiVersion; }
    public List<String> getAuthors()    { return authors; }
    public List<String> getDepend()     { return depend; }
    public List<String> getSoftdepend() { return softdepend; }

    @Override
    public String toString() {
        return name + " v" + version;
    }
}
