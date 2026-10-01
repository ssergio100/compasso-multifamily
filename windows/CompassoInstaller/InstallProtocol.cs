using System.Text.Json;
using System.Text.Json.Serialization;

namespace CompassoInstaller;

/// <summary>
/// Contrato do canal duplex entre a interface não elevada e o trabalhador elevado.
/// Uma mensagem por linha, serializada como JSON, para que o pipe nunca dependa
/// de limites de quadro: a leitura só é concluída quando o delimitador aparece.
/// </summary>
internal static class InstallProtocol
{
    internal const string WorkerArgument = "--elevated-probe";
    internal const string UnattendedInstallArgument = "--install-unattended";
    internal const string PipePrefix = "compasso-installer-";

    /// <summary>Teto para o processo elevado abrir o pipe. Cobre a espera pelo UAC.</summary>
    internal static readonly TimeSpan ConnectTimeout = TimeSpan.FromSeconds(60);

    /// <summary>Teto para o anúncio de permissão depois que o pipe abriu.</summary>
    internal static readonly TimeSpan HandshakeTimeout = TimeSpan.FromSeconds(30);

    /// <summary>Teto de uma instalação. Cópia de payload, não deveria chegar perto disso.</summary>
    internal static readonly TimeSpan OperationTimeout = TimeSpan.FromMinutes(10);

    /// <summary>Margem para o trabalhador perceber o pedido de encerramento.</summary>
    internal static readonly TimeSpan ShutdownTimeout = TimeSpan.FromSeconds(5);
}

internal static class MessageType
{
    /// <summary>O trabalhador anunciado que conectou e confirma que tem permissão administrativa.</summary>
    internal const string Ready = "ready";

    /// <summary>Step completed; <see cref="WorkerMessage.Percent"/> is the overall completion ratio.</summary>
    internal const string Progress = "progress";

    /// <summary>Terminal answer to a command the worker accepted.</summary>
    internal const string Result = "result";

    /// <summary>Terminal answer to a command the worker could not even attempt.</summary>
    internal const string Fault = "fault";

    internal const string Install = "install";
    internal const string Uninstall = "uninstall";
    internal const string Shutdown = "shutdown";
}

/// <summary>
/// Envelope único para os dois sentidos do canal. A separação entre
/// <see cref="Type"/> e <see cref="Succeeded"/> é deliberada: <see cref="Type"/>
/// decide qual o campo carrega, e os campos ausentes simplesmente não existem
/// na linha em vez de valerem zero.
/// </summary>
internal sealed class WorkerMessage
{
    public string Type { get; set; } = string.Empty;
    public bool Elevated { get; set; }
    public int Percent { get; set; }
    public bool Succeeded { get; set; }
    public string Text { get; set; } = string.Empty;
    public InstallPlan? Plan { get; set; }
}

/// <summary>
/// Fonte dos componentes a gravar. Relativa à pasta de payload do instalador,
/// calculada a partir do diretório do executável, porque o processo elevado é
/// relançado a partir do mesmo lugar e precisa achar a mesma árvore.
/// </summary>
internal sealed class InstallPlan
{
    public string ProductName { get; set; } = string.Empty;
    public string Version { get; set; } = string.Empty;
    public string InstallDirectory { get; set; } = string.Empty;
    public string StartMenuDirectory { get; set; } = string.Empty;
    public List<PayloadFile> Files { get; set; } = [];
    public List<PayloadShortcut> Shortcuts { get; set; } = [];
    public UninstallEntry Uninstall { get; set; } = new();
}

internal sealed class PayloadFile
{
    public string Source { get; set; } = string.Empty;
    public string Destination { get; set; } = string.Empty;
}

internal sealed class PayloadShortcut
{
    public string Target { get; set; } = string.Empty;
    public string ShortcutPath { get; set; } = string.Empty;
    public string? Description { get; set; }
    public string? IconLocation { get; set; }
}

internal sealed class UninstallEntry
{
    public string KeyPath { get; set; } = string.Empty;
    public string DisplayName { get; set; } = string.Empty;
    public string DisplayVersion { get; set; } = string.Empty;
    public string Publisher { get; set; } = string.Empty;
    public string UninstallString { get; set; } = string.Empty;
    public int NoModify { get; set; } = 1;
    public int NoRepair { get; set; } = 1;
}

/// <summary>
/// Contexto de serialização gerado em tempo de compilação. O projeto publica com
/// <c>PublishTrimmed</c> ligado, e a serialização por reflexão seria removida
/// pelo trimmer em Release, quebrando o canal só na build distribuível.
/// </summary>
[JsonSourceGenerationOptions(
    PropertyNamingPolicy = JsonKnownNamingPolicy.CamelCase,
    DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull)]
[JsonSerializable(typeof(WorkerMessage))]
internal sealed partial class WorkerMessageContext : JsonSerializerContext
{
}
