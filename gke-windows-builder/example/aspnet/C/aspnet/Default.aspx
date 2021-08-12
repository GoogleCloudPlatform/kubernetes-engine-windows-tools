<%@ Page Language="C#" %>
<%
    HelloWorldLabel.Text = "Hello world!!!!!";
%>
<html>
<head runat="server">
    <title>oh myyyy</title>
</head>
<body>
    <form id="form" runat="server">
    <div>
    <asp:Label runat="server" id="HelloWorldLabel"></asp:Label>
    </div>
    </form>
</body>
</html>